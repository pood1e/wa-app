import { describe, expect, it } from 'vitest';
import { ClientPlatform } from '../proto/byte/v/forge/waapp/protocol/v1/account';
import { AccountAddStatus, type AccountAddFlow } from '../proto/byte/v/forge/waapp/v1/account_add';
import { WaErrorCode } from '../proto/byte/v/forge/waapp/v1/common';
import { AccountProbeStatus, VerificationDeliveryMethod as Method, VerificationRequestStatus } from '../proto/byte/v/forge/waapp/v1/registration';
import { addingError, addingStatus, canRecoverRegistration, canSubmitCode, canSubmitPassword, channelState, isTransferMethod } from './wa-account-add-state';

const observed = '2026-01-01T00:00:00Z';
const start = Date.parse(observed);

function readyFlow(): AccountAddFlow {
  return {
    flow_id: 'synthetic-flow', phone: undefined, platform: ClientPlatform.CLIENT_PLATFORM_ANDROID, password_challenge: undefined,
    status: AccountAddStatus.ACCOUNT_ADD_STATUS_READY, source: { registration: {} },
    probe: {
      account_probe_id: '', wa_account_id: '', client_profile_id: '',
      status: AccountProbeStatus.ACCOUNT_PROBE_STATUS_REACHABLE,
      supported_methods: [Method.VERIFICATION_DELIVERY_METHOD_SMS, Method.VERIFICATION_DELIVERY_METHOD_VOICE],
      method_statuses: [], probed_at: observed, last_error: undefined,
    },
    verification: undefined, transfer_challenge: undefined, account: undefined, expires_at: undefined, last_error: undefined,
  };
}

describe('verification availability', () => {
  it('uses the password step after first-factor verification and for unknown password results', () => {
    const flow = readyFlow();
    flow.status = AccountAddStatus.ACCOUNT_ADD_STATUS_WAITING_PASSWORD;
    flow.password_challenge = { next_attempt_at: undefined };
    expect(canSubmitPassword(flow)).toBe(true);
    expect(canSubmitCode(flow)).toBe(false);
    expect(canRecoverRegistration(flow)).toBe(false);
    expect(addingStatus(flow)).toContain('两步验证密码');
    flow.status = AccountAddStatus.ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN;
    expect(canSubmitPassword(flow)).toBe(true);
    expect(canSubmitCode(flow)).toBe(false);
    expect(canRecoverRegistration(flow)).toBe(true);
    flow.last_error = { code: WaErrorCode.WA_ERROR_CODE_RATE_LIMITED, message: 'synthetic cooldown', retryable: true };
    expect(addingError(flow, start)).toContain('两步验证');
    flow.status = AccountAddStatus.ACCOUNT_ADD_STATUS_ACCEPTED;
    expect(canSubmitPassword(flow)).toBe(false);
  });
  it.each([AccountAddStatus.ACCOUNT_ADD_STATUS_WAITING_CODE, AccountAddStatus.ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN])('requires confirmed delivery before submitting in state %s', (status) => {
    const flow = readyFlow();
    flow.status = status;
    expect(canSubmitCode(flow)).toBe(false);
    flow.verification = {
      verification_request_id: 'synthetic-verification', wa_account_id: '', client_profile_id: '',
      delivery_method: Method.VERIFICATION_DELIVERY_METHOD_WA_OLD,
      status: VerificationRequestStatus.VERIFICATION_REQUEST_STATUS_REJECTED,
      expected_code_length: 6, requested_at: observed, expires_at: undefined,
      last_error: undefined, retry_after: '0s', method_statuses: [],
    };
    expect(canSubmitCode(flow)).toBe(false);
    flow.verification.status = VerificationRequestStatus.VERIFICATION_REQUEST_STATUS_SENT;
    expect(canSubmitCode(flow)).toBe(true);
  });

  it('offers fresh verification after missing or stale responses while preserving the cooldown', () => {
    const flow = readyFlow();
    flow.source = { transfer: {} };
    flow.probe!.supported_methods = [Method.VERIFICATION_DELIVERY_METHOD_WA_OLD];
    flow.last_error = { code: WaErrorCode.WA_ERROR_CODE_VERIFICATION_REQUIRED, message: 'synthetic missing verification', retryable: true };
    flow.verification = {
      verification_request_id: 'synthetic-rejected', wa_account_id: '', client_profile_id: '',
      delivery_method: Method.VERIFICATION_DELIVERY_METHOD_WA_OLD,
      status: VerificationRequestStatus.VERIFICATION_REQUEST_STATUS_REJECTED,
      expected_code_length: 6, requested_at: observed, expires_at: undefined,
      last_error: flow.last_error, retry_after: '210s', method_statuses: [],
    };
    expect(canSubmitCode(flow)).toBe(false);
    expect(canRecoverRegistration(flow)).toBe(false);
    expect(channelState(flow, Method.VERIFICATION_DELIVERY_METHOD_WA_OLD, start + 30_000)).toEqual({ available: false, remaining: 180 });
    expect(addingError(flow, start + 30_000)).toContain('冷却');
    expect(channelState(flow, Method.VERIFICATION_DELIVERY_METHOD_WA_OLD, start + 210_000)).toEqual({ available: true, remaining: 0 });
    expect(addingError(flow, start + 210_000)).toContain('本次收到的新验证码');
  });

  it('honors the global retry delay even when the latest response omits method statuses', () => {
    const flow = readyFlow();
    flow.verification = {
      verification_request_id: '', wa_account_id: '', client_profile_id: '',
      delivery_method: Method.VERIFICATION_DELIVERY_METHOD_SMS,
      status: VerificationRequestStatus.VERIFICATION_REQUEST_STATUS_REJECTED,
      expected_code_length: 6, requested_at: observed, expires_at: undefined,
      last_error: undefined, retry_after: '300s', method_statuses: [],
    };
    for (const method of flow.probe!.supported_methods) {
      expect(channelState(flow, method, start + 12_000)).toEqual({ available: false, remaining: 288 });
      expect(channelState(flow, method, start + 300_000)).toEqual({ available: true, remaining: 0 });
    }
    flow.last_error = { code: WaErrorCode.WA_ERROR_CODE_RATE_LIMITED, message: 'synthetic cooldown', retryable: true };
    expect(addingError(flow, start)).toContain('冷却');
    expect(addingError(flow, start + 300_000)).toContain('重新提交');
  });

  it('keeps the longer method cooldown after the global delay ends', () => {
    const flow = readyFlow();
    flow.probe!.method_statuses = [{ delivery_method: Method.VERIFICATION_DELIVERY_METHOD_SMS, available: false, cooldown: '600s' }];
    expect(channelState(flow, Method.VERIFICATION_DELIVERY_METHOD_SMS, start + 300_000)).toEqual({ available: false, remaining: 300 });
    expect(channelState(flow, Method.VERIFICATION_DELIVERY_METHOD_SMS, start + 600_000)).toEqual({ available: true, remaining: 0 });
  });

  it('does not enable an explicitly unavailable method', () => {
    const flow = readyFlow();
    flow.probe!.method_statuses = [{ delivery_method: Method.VERIFICATION_DELIVERY_METHOD_SMS, available: false, cooldown: '0s' }];
    expect(channelState(flow, Method.VERIFICATION_DELIVERY_METHOD_SMS, start + 600_000)).toEqual({ available: false, remaining: 0 });
  });

  it('blocks QR transfer even when an upstream response advertises it', () => {
    const flow = readyFlow();
    flow.source = { transfer: {} };
    flow.probe!.supported_methods = [Method.VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER];
    flow.probe!.method_statuses = [
      { delivery_method: Method.VERIFICATION_DELIVERY_METHOD_WA_OLD, available: false, cooldown: '0s' },
      { delivery_method: Method.VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER, available: true, cooldown: '0s' },
    ];
    expect(isTransferMethod(Method.VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER)).toBe(true);
    expect(channelState(flow, Method.VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER, start).available).toBe(false);
    expect(channelState(flow, Method.VERIFICATION_DELIVERY_METHOD_WA_OLD, start).available).toBe(false);
    flow.verification = {
      verification_request_id: '', wa_account_id: '', client_profile_id: '',
      delivery_method: Method.VERIFICATION_DELIVERY_METHOD_ACCOUNT_TRANSFER,
      status: VerificationRequestStatus.VERIFICATION_REQUEST_STATUS_WAITING,
      expected_code_length: 6, requested_at: observed, expires_at: undefined,
      last_error: undefined, retry_after: '0s', method_statuses: [],
    };
    flow.status = AccountAddStatus.ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN;
    flow.verification.expires_at = new Date(start - 1).toISOString();
    expect(canRecoverRegistration(flow)).toBe(true);
    flow.status = AccountAddStatus.ACCOUNT_ADD_STATUS_WAITING_CODE;
    expect(canRecoverRegistration(flow)).toBe(true);
    flow.status = AccountAddStatus.ACCOUNT_ADD_STATUS_OUTCOME_UNKNOWN;
    flow.verification.status = VerificationRequestStatus.VERIFICATION_REQUEST_STATUS_REJECTED;
    expect(canRecoverRegistration(flow)).toBe(false);
  });
});
