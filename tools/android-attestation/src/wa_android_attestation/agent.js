function run(operation) {
  return new Promise((resolve) =>
    Java.perform(() => {
      try {
        resolve(operation());
      } catch {
        resolve({ error: "native_unavailable" });
      }
    }),
  );
}

function identity() {
  const app = Java.use("android.app.ActivityThread").currentApplication();
  if (app === null || app.getPackageName().toString() !== "com.whatsapp")
    throw new Error("application_context_unavailable");
  return app;
}

function bytes(hex) {
  if (!/^(?:[0-9a-f]{2})+$/.test(hex)) throw new Error("invalid_bytes");
  return Java.array(
    "byte",
    hex.match(/../g).map((part) => {
      const value = Number.parseInt(part, 16);
      return value > 127 ? value - 256 : value;
    }),
  );
}

function encode(value) {
  return Java.use("android.util.Base64").encodeToString(value, 2).toString();
}

function aliasFor(id) {
  if (!/^[0-9a-f]{32}$/.test(id)) throw new Error("invalid_context");
  return "wa_account_add_" + id;
}

function keyStore() {
  const store = Java.use("java.security.KeyStore").getInstance(
    "AndroidKeyStore",
  );
  store.load
    .overload("java.security.KeyStore$LoadStoreParameter")
    .call(store, null);
  return store;
}

function taskResult(task) {
  const Tasks = Java.use("com.google.android.gms.tasks.Tasks");
  const TimeUnit = Java.use("java.util.concurrent.TimeUnit");
  try {
    Tasks.await
      .overload(
        "com.google.android.gms.tasks.Task",
        "long",
        "java.util.concurrent.TimeUnit",
      )
      .call(Tasks, task, 25, TimeUnit.SECONDS.value);
  } catch {
    if (!task.isComplete()) throw { sdkCode: 1004 };
  }
  if (task.isSuccessful()) return task.getResult();
  const exception = task.getException();
  const StandardException = Java.use(
    "com.google.android.play.core.integrity.StandardIntegrityException",
  );
  if (exception !== null && StandardException.class.isInstance(exception))
    throw {
      sdkCode: Number(Java.cast(exception, StandardException).getErrorCode()),
    };
  const ApiException = Java.use(
    "com.google.android.gms.common.api.ApiException",
  );
  if (exception !== null && ApiException.class.isInstance(exception))
    throw {
      sdkCode: Number(Java.cast(exception, ApiException).getStatusCode()),
    };
  throw { sdkCode: 1000 };
}

rpc.exports = {
  create(id, publicHex) {
    return run(() => {
      identity();
      if (publicHex.length !== 64) throw new Error("invalid_public_key");
      const alias = aliasFor(id);
      const store = keyStore();
      if (store.containsAlias(alias))
        throw new Error("context_already_created");
      const challenge =
        Math.floor(Date.now() / 1000)
          .toString(16)
          .padStart(16, "0") +
        "1f" +
        publicHex;
      const Builder = Java.use(
        "android.security.keystore.KeyGenParameterSpec$Builder",
      );
      const builder = Builder.$new(alias, 4);
      builder.setAlgorithmParameterSpec(
        Java.use("java.security.spec.ECGenParameterSpec").$new("secp256r1"),
      );
      builder.setDigests(
        Java.array("java.lang.String", ["SHA-256", "SHA-512"]),
      );
      builder.setUserAuthenticationRequired(false);
      builder.setAttestationChallenge(bytes(challenge));
      builder.setCertificateNotAfter(
        Java.use("java.util.Date").$new(Date.now() + 86400000),
      );
      builder.setDevicePropertiesAttestationIncluded(true);
      const generator = Java.use("java.security.KeyPairGenerator").getInstance(
        "EC",
        "AndroidKeyStore",
      );
      const initialize = () =>
        generator.initialize
          .overload("java.security.spec.AlgorithmParameterSpec")
          .call(generator, builder.build());
      try {
        initialize();
        generator.generateKeyPair();
      } catch {
        builder.setDevicePropertiesAttestationIncluded(false);
        initialize();
        generator.generateKeyPair();
      }
      return {
        certificates: Array.from(
          store.getCertificateChain(alias),
          (certificate) => encode(certificate.getEncoded()),
        ),
      };
    });
  },
  sign(id, messageHex) {
    return run(() => {
      identity();
      const store = keyStore();
      const alias = aliasFor(id);
      if (!store.containsAlias(alias)) return { error: "context_not_ready" };
      const signer = Java.use("java.security.Signature").getInstance(
        "SHA256withECDSA",
      );
      signer.initSign(
        Java.cast(
          store.getKey(alias, null),
          Java.use("java.security.PrivateKey"),
        ),
      );
      signer.update.overload("[B").call(signer, bytes(messageHex));
      return { signature: encode(signer.sign()) };
    });
  },
  integrity(publicHex) {
    return run(() => {
      const app = identity();
      if (publicHex.length !== 64) throw new Error("invalid_public_key");
      try {
        const pkg = "com.google.android.play.core.integrity.";
        const factory = Java.use(pkg + "IntegrityManagerFactory");
        const manager = factory.createStandard(app);
        const prepare = Java.use(
          pkg + "StandardIntegrityManager$PrepareIntegrityTokenRequest",
        );
        const builder = prepare.builder();
        builder.setCloudProjectNumber(293955441834);
        const provider = Java.cast(
          taskResult(manager.prepareIntegrityToken(builder.build())),
          Java.use(
            pkg + "StandardIntegrityManager$StandardIntegrityTokenProvider",
          ),
        );
        const request = Java.use(
          pkg + "StandardIntegrityManager$StandardIntegrityTokenRequest",
        ).builder();
        const hash = Java.use("android.util.Base64")
          .encodeToString(bytes(publicHex), 3)
          .toString();
        request.setRequestHash(hash);
        const token = Java.cast(
          taskResult(provider.request(request.build())),
          Java.use(pkg + "StandardIntegrityManager$StandardIntegrityToken"),
        );
        return { token: token.token().toString() };
      } catch (failure) {
        if (Number.isInteger(failure.sdkCode) && failure.sdkCode !== 0)
          return { errorCode: failure.sdkCode };
        throw failure;
      }
    });
  },
  release(id) {
    return run(() => {
      identity();
      const store = keyStore();
      const alias = aliasFor(id);
      if (store.containsAlias(alias)) store.deleteEntry(alias);
      return {};
    });
  },
};
