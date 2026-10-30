import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = readFileSync(new URL("../src/wa_ios_attestation/agent.js", import.meta.url), "utf8");
const pointer = (value) => ({ value, isNull: () => value === 0 });
const nil = pointer(0);
const flush = () => new Promise((resolve) => setImmediate(resolve));

function agent(shared = { active: false, callbacks: [], owners: [], fail: false }) {
  let pins = 0;
  let pools = 0;
  const bundle = {
    handle: pointer(1),
    bundleIdentifier: () => shared.bundleIdentifier || "net.whatsapp.WhatsApp",
    objectForInfoDictionaryKey_: () => { throw new Error("version metadata must not be requested"); },
  };
  const service = {
    isSupported: () => true,
    "- generateKeyWithCompletionHandler:": (callback) => {
      if (shared.fail) throw new Error("PRIVATE_FAILURE");
      shared.owners.push(callback.retain());
      shared.callbacks.push(callback);
    },
  };
  const natives = {
    objc_sync_enter: () => 0,
    objc_sync_exit: () => 0,
    objc_getAssociatedObject: () => pointer(shared.active ? 1 : 0),
    objc_setAssociatedObject: (_host, _key, value) => {
      shared.active = !value.isNull();
    },
  };
  const sandbox = vm.createContext({
    Process: { mainModule: { name: "WhatsApp" } },
    Module: { getGlobalExportByName: (name) => name },
    NativeFunction: function (name) {
      return natives[name];
    },
    retainedBlock(specification, onDispose) {
      if (shared.failRetain) throw new Error("PRIVATE_FAILURE");
      pins++;
      let references = 1;
      const release = () => {
        if (--references === 0) {
          setImmediate(() => {
            try { onDispose(); } finally { pins--; }
          });
        }
      };
      return {
        handle: {
          ...specification,
          retain() { references++; return release; },
        },
        release,
      };
    },
    ObjC: {
      available: true,
      selector: (name) => name,
      Object: function (value) {
        return value;
      },
      classes: {
        NSAutoreleasePool: {
          alloc: () => ({ init: () => { pools++; return { release: () => pools-- }; } }),
        },
        NSBundle: { mainBundle: () => bundle },
        NSNumber: { numberWithBool_: () => ({ handle: pointer(1) }) },
        DCAppAttestService: { sharedService: () => service },
      },
    },
    ptr: pointer,
    rpc: { exports: {} },
  });
  vm.runInContext(source, sandbox);
  return { rpc: sandbox.rpc.exports, pins: () => pins, pools: () => pools, shared };
}

function complete(
  shared,
  error = nil,
  value = { handle: pointer(1), toString: () => "synthetic-key" },
) {
  shared.callbacks.shift().implementation(value, error);
}

async function dispose(shared) {
  shared.owners.shift()();
  await flush();
}

test("SDK final release controls script pinning and cross-agent exclusion", async () => {
  const first = agent();
  const pending = first.rpc.create();
  assert.equal(first.pins(), 1);
  const second = agent(first.shared);
  assert.equal((await second.rpc.create()).error, "device_busy");
  assert.equal(first.shared.callbacks.length, 1);
  complete(first.shared);
  assert.equal((await pending).keyIdentifier, "synthetic-key");
  assert.equal(first.pins(), 1);
  assert.equal(first.shared.active, true);
  assert.equal((await second.rpc.create()).error, "device_busy");
  await dispose(first.shared);
  assert.equal(first.pins(), 0);
  assert.equal(first.shared.active, false);
  assert.equal(first.pools(), 0);
  const next = second.rpc.create();
  complete(second.shared);
  assert.equal((await next).keyIdentifier, "synthetic-key");
  await dispose(second.shared);
  assert.equal(second.pins(), 0);
  assert.equal(second.pools(), 0);
});

test("SDK failures release both the callback and device guard without native error text", async () => {
  const instance = agent();
  instance.shared.fail = true;
  assert.equal((await instance.rpc.create()).error, "context_not_ready");
  await flush();
  assert.equal(instance.pins(), 0);
  assert.equal(instance.shared.active, false);
  instance.shared.fail = false;
  const pending = instance.rpc.create();
  complete(instance.shared, {
    handle: pointer(1),
    domain: () => "com.apple.devicecheck.error",
    code: () => 4,
  });
  assert.equal((await pending).error, "server_unavailable");
  assert.equal(instance.pins(), 1);
  await dispose(instance.shared);
  assert.equal(instance.pins(), 0);
  assert.equal(instance.shared.active, false);
  assert.equal(instance.pools(), 0);
});

test("encoding failures retain native ownership until final SDK release", async () => {
  const instance = agent();
  const pending = instance.rpc.create();
  complete(instance.shared, nil, {
    handle: pointer(1),
    toString: () => {
      throw new Error("PRIVATE_FAILURE");
    },
  });
  const result = await pending;
  assert.equal(result.error, "context_not_ready");
  assert.equal(JSON.stringify(result).includes("PRIVATE"), false);
  assert.equal(instance.pins(), 1);
  await dispose(instance.shared);
  assert.equal(instance.pins(), 0);
  assert.equal(instance.shared.active, false);
});

test("failed block creation releases the device guard", async () => {
  const instance = agent();
  instance.shared.failRetain = true;
  assert.equal((await instance.rpc.create()).error, "context_not_ready");
  assert.equal(instance.pins(), 0);
  assert.equal(instance.shared.active, false);
  assert.equal(instance.pools(), 0);
});

test("key creation does not require application version metadata", async () => {
  const instance = agent();
  const pending = instance.rpc.create();
  complete(instance.shared);
  const result = await pending;
  assert.equal(result.keyIdentifier, "synthetic-key");
  assert.equal(result.bundleIdentifier, "net.whatsapp.WhatsApp");
  assert.equal(Object.hasOwn(result, "appVersion"), false);
  await dispose(instance.shared);
});

test("another application cannot create a proof or use a bound key", async () => {
  const instance = agent();
  instance.shared.bundleIdentifier = "another.app";
  assert.equal((await instance.rpc.create()).error, "context_not_ready");
  instance.shared.bundleIdentifier = "net.whatsapp.WhatsApp";
  assert.equal((await instance.rpc.sign("synthetic-key", "another.app", "00".repeat(32))).error, "context_not_ready");
  assert.equal(instance.shared.callbacks.length, 0);
  assert.equal(instance.pools(), 0);
});
