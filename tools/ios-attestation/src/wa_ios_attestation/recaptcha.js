/* reCAPTCHA Enterprise SDK login proof; no registration or app navigation. */
function recaptchaLogin(siteKey) {
  if (typeof siteKey !== "string" || !/^[A-Za-z0-9_-]{20,256}$/.test(siteKey))
    throw new Error("invalid_site_key");
  return inAutoreleasePool(() => {
    identity();
    return new Promise((resolve) => {
      const release = claimDevice();
      if (release === null) return resolve({ error: "device_busy" });
      let forwarded = false;
      let initialized;
      try {
        initialized = retainedBlock({
          retType: "void", argTypes: ["object", "object"],
          implementation(pointer, error) {
            inAutoreleasePool(() => {
              if (nil(pointer) || !nil(error)) {
                resolve({ error: "context_not_ready" });
                return;
              }
              const client = new ObjC.Object(pointer).retain();
              let completion;
              try {
                completion = retainedBlock({
                  retType: "void", argTypes: ["object", "object"],
                  implementation(token, failure) {
                    inAutoreleasePool(() => {
                      try {
                        resolve(nil(token) || !nil(failure)
                          ? { error: "context_not_ready" }
                          : { token: new ObjC.Object(token).toString() });
                      } finally {
                        client.release();
                      }
                    });
                  },
                }, release);
                forwarded = true;
                const action = ObjC.classes._TtC19RecaptchaEnterprise15RecaptchaAction.login();
                client.execute_completion_(action, completion.handle);
              } catch {
                client.release();
                resolve({ error: "context_not_ready" });
              } finally {
                if (completion) completion.release();
              }
            });
          },
        }, () => { if (!forwarded) release(); });
        ObjC.classes._TtC19RecaptchaEnterprise9Recaptcha
          .fetchClientWithSiteKey_completion_(siteKey, initialized.handle);
      } catch {
        if (!initialized) release();
        resolve({ error: "context_not_ready" });
      } finally {
        if (initialized) initialized.release();
      }
    });
  });
}

rpc.exports.recaptcha = recaptchaLogin;
