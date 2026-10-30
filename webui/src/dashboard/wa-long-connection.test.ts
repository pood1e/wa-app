import { describe, expect, it } from 'vitest';
import { WaErrorCode } from '../proto/byte/v/forge/waapp/v1/common';
import { LongConnectionStatus, type LongConnectionState } from '../proto/byte/v/forge/waapp/v1/messaging';
import { WAAccountStatus } from '../proto/byte/v/forge/waapp/v1/profile';
import { indexConnections, waAccountDisplayStatus } from './wa-long-connection-badge';

describe('account connection status', () => {
  it('keeps the latest rejection visible among historical stopped identities in either order', () => {
    const historical = connection({ login_state_id: 'old' });
    const rejected = connection({ login_state_id: 'latest', last_heartbeat_at: '2026-10-20T12:00:00Z', last_error: { code: WaErrorCode.WA_ERROR_CODE_AUTHENTICATION_FAILED, message: 'rejected', retryable: false } });
    for (const entries of [[historical, rejected], [rejected, historical]]) {
      const current = indexConnections(entries).get('fixture');
      expect(current?.login_state_id).toBe('latest');
      expect(waAccountDisplayStatus({ wa_account_id: 'fixture', display_name: '', phone: undefined, status: WAAccountStatus.WA_ACCOUNT_STATUS_ACTIVE, audit: undefined, two_factor_auth: undefined }, current)).toMatchObject({ label: '登录已失效', tone: 'bad' });
    }
  });
  it('uses an active identity over stopped identities restored after a restart', () => {
    const old = connection({ last_error: { code: WaErrorCode.WA_ERROR_CODE_AUTHENTICATION_FAILED, message: 'rejected', retryable: false } });
    const active = connection({ login_state_id: 'active', status: LongConnectionStatus.LONG_CONNECTION_STATUS_HEARTBEAT_WAITING });
    expect(indexConnections([old, active]).get('fixture')?.login_state_id).toBe('active');
  });
});

function connection(overrides: Partial<LongConnectionState>): LongConnectionState {
  return { login_state_id: '', wa_account_id: 'fixture', client_profile_id: '', registered_identity_id: '', message_session_id: '', status: LongConnectionStatus.LONG_CONNECTION_STATUS_STOPPED, heartbeat_supported: true, started_at: undefined, last_connected_at: undefined, last_heartbeat_at: undefined, last_message_at: undefined, reconnect_count: 0, last_error: undefined, ...overrides };
}
