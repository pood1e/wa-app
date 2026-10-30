/* DeviceCheck primitives in the WhatsApp app. */
function inAutoreleasePool(operation) {
  const pool = ObjC.classes.NSAutoreleasePool.alloc().init();
  try {
    return operation();
  } finally {
    pool.release();
  }
}

function nil(value) {
  return (
    value === null ||
    (value.handle === undefined ? value.isNull() : value.handle.isNull())
  );
}

function identity() {
  if (Process.mainModule.name !== "WhatsApp" || !ObjC.available)
    throw new Error("unsupported_process");
  const bundle = ObjC.classes.NSBundle.mainBundle();
  const bundleIdentifier = bundle.bundleIdentifier().toString();
  if (bundleIdentifier !== "net.whatsapp.WhatsApp")
    throw new Error("unsupported_app");
  return { bundleIdentifier };
}

function hashData(hex) {
  if (!/^[0-9a-f]{64}$/.test(hex)) throw new Error("invalid_hash");
  const bytes = hex.match(/../g).map((value) => Number.parseInt(value, 16));
  const memory = Memory.alloc(32);
  memory.writeByteArray(bytes);
  return ObjC.classes.NSData.dataWithBytes_length_(memory, 32);
}

function claimDevice() {
  const host = ObjC.classes.NSBundle.mainBundle();
  const key = ObjC.selector("waAppAttestWorkerActiveCall");
  const native = (name, result, args) =>
    new NativeFunction(Module.getGlobalExportByName(name), result, args);
  const enter = native("objc_sync_enter", "int", ["pointer"]);
  const exit = native("objc_sync_exit", "int", ["pointer"]);
  const get = native("objc_getAssociatedObject", "pointer", [
    "pointer",
    "pointer",
  ]);
  const set = native("objc_setAssociatedObject", "void", [
    "pointer",
    "pointer",
    "pointer",
    "ulong",
  ]);
  enter(host.handle);
  try {
    if (!get(host.handle, key).isNull()) return null;
    set(
      host.handle,
      key,
      ObjC.classes.NSNumber.numberWithBool_(true).handle,
      1,
    );
  } finally {
    exit(host.handle);
  }
  return () => {
    enter(host.handle);
    try {
      set(host.handle, key, ptr(0), 1);
    } finally {
      exit(host.handle);
    }
  };
}

function invoke(selector, args, encode) {
  const service = ObjC.classes.DCAppAttestService.sharedService();
  if (!service.isSupported()) return { error: "context_not_ready" };
  return new Promise((resolve) => {
    const release = claimDevice();
    if (release === null) {
      resolve({ error: "device_busy" });
      return;
    }
    let callback;
    try {
      callback = retainedBlock(
        {
          retType: "void",
          argTypes: ["object", "object"],
          implementation(value, error) {
            inAutoreleasePool(() => {
              try {
                if (!nil(error)) {
                  const failure = new ObjC.Object(error);
                  const unavailable =
                    failure.domain().toString() ===
                      "com.apple.devicecheck.error" &&
                    Number(failure.code()) === 4;
                  resolve({
                    error: unavailable
                      ? "server_unavailable"
                      : "context_not_ready",
                  });
                } else {
                  resolve(
                    nil(value)
                      ? { error: "context_not_ready" }
                      : encode(new ObjC.Object(value)),
                  );
                }
              } catch {
                resolve({ error: "context_not_ready" });
              }
            });
          },
        },
        release,
      );
    } catch {
      release();
      resolve({ error: "context_not_ready" });
      return;
    }
    try {
      service[selector](...args, callback.handle);
    } catch {
      resolve({ error: "context_not_ready" });
    } finally {
      callback.release();
    }
  });
}

function prove(selector, keyIdentifier, bundleIdentifier, hashHex) {
  return inAutoreleasePool(() => {
    try {
      const app = identity();
      if (
        app.bundleIdentifier !== bundleIdentifier
      )
        return { error: "context_not_ready" };
      return invoke(selector, [keyIdentifier, hashData(hashHex)], (value) => ({
        proof: value.base64EncodedStringWithOptions_(0).toString(),
      }));
    } catch {
      return { error: "context_not_ready" };
    }
  });
}

rpc.exports = {
  create() {
    return inAutoreleasePool(() => {
      try {
        const app = identity();
        return invoke("- generateKeyWithCompletionHandler:", [], (value) => ({
          ...app,
          keyIdentifier: value.toString(),
        }));
      } catch {
        return { error: "context_not_ready" };
      }
    });
  },

  attest(keyIdentifier, bundleIdentifier, hashHex) {
    return prove(
      "- attestKey:clientDataHash:completionHandler:",
      keyIdentifier,
      bundleIdentifier,
      hashHex,
    );
  },

  sign(keyIdentifier, bundleIdentifier, hashHex) {
    return prove(
      "- generateAssertion:clientDataHash:completionHandler:",
      keyIdentifier,
      bundleIdentifier,
      hashHex,
    );
  },
};
