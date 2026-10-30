import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = readFileSync(new URL("../src/wa_ios_attestation/recaptcha.js", import.meta.url), "utf8");
const flush = () => new Promise((resolve) => setImmediate(resolve));

function agent() {
  const state = { busy: false, fetches: [], executions: [], owners: [], pins: 0, retains: 0 };
  const client = {
    retain() { state.retains++; return this; },
    release() { state.retains--; },
    execute_completion_(action, callback) {
      assert.equal(action, "login");
      state.executions.push(callback);
      state.owners.push(callback.retain());
    },
  };
  const sandbox = vm.createContext({
    inAutoreleasePool: (call) => call(),
    identity: () => ({}),
    nil: (value) => value === null,
    claimDevice: () => {
      if (state.busy) return null;
      state.busy = true;
      return () => { state.busy = false; };
    },
    retainedBlock(specification, onDispose) {
      state.pins++;
      let references = 1;
      const release = () => {
        if (--references === 0) setImmediate(() => { onDispose(); state.pins--; });
      };
      return {
        handle: { ...specification, retain: () => { references++; return release; } },
        release,
      };
    },
    ObjC: {
      Object: function (value) { return value; },
      classes: {
        _TtC19RecaptchaEnterprise9Recaptcha: {
          fetchClientWithSiteKey_completion_(key, callback) {
            state.fetches.push({ key, callback });
            state.owners.push(callback.retain());
          },
        },
        _TtC19RecaptchaEnterprise15RecaptchaAction: { login: () => "login" },
      },
    },
    rpc: { exports: {} },
  });
  vm.runInContext(source, sandbox);
  return { state, client, generate: sandbox.rpc.exports.recaptcha };
}

test("SDK token uses the supplied site key and holds the device through final release", async () => {
  const { state, client, generate } = agent();
  const pending = generate("synthetic-key-1234567890");
  assert.equal(state.fetches[0].key, "synthetic-key-1234567890");
  state.fetches[0].callback.implementation(client, null);
  state.owners[0]();
  await flush();
  assert.equal(state.retains, 1);
  assert.equal(state.busy, true);
  const competing = await generate("synthetic-key-1234567890");
  assert.equal(competing.error, "device_busy");
  state.executions[0].implementation({ toString: () => "synthetic-token" }, null);
  assert.equal((await pending).token, "synthetic-token");
  assert.equal(state.retains, 0);
  assert.equal(state.busy, true);
  state.owners[1]();
  await flush();
  assert.equal(state.busy, false);
  assert.equal(state.pins, 0);
});

test("initialization failure releases the device and omits native errors", async () => {
  const { state, generate } = agent();
  const pending = generate("synthetic-key-1234567890");
  state.fetches[0].callback.implementation(null, { private: "PRIVATE_FAILURE" });
  assert.equal((await pending).error, "context_not_ready");
  state.owners[0]();
  await flush();
  assert.equal(state.busy, false);
  assert.equal(state.pins, 0);
});

test("execution failure releases the retained SDK client", async () => {
  const { state, client, generate } = agent();
  const pending = generate("synthetic-key-1234567890");
  state.fetches[0].callback.implementation(client, null);
  state.executions[0].implementation(null, { private: "PRIVATE_FAILURE" });
  assert.equal((await pending).error, "context_not_ready");
  state.owners.forEach((release) => release());
  await flush();
  assert.equal(state.retains, 0);
  assert.equal(state.busy, false);
  assert.equal(state.pins, 0);
});
