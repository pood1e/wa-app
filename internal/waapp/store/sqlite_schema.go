package store

const SQLiteStoreSchema = `
CREATE TABLE IF NOT EXISTS wa_sqlite_account_proxies (
 wa_account_id TEXT PRIMARY KEY REFERENCES wa_sqlite_accounts(id) ON DELETE CASCADE,
 configuration BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS wa_sqlite_account_credentials (
 client_profile_id TEXT PRIMARY KEY REFERENCES wa_sqlite_client_profiles(id) ON DELETE CASCADE,
 credentials BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS wa_sqlite_protocol_states (
 client_profile_id TEXT PRIMARY KEY REFERENCES wa_sqlite_client_profiles(id) ON DELETE CASCADE,
 recovery BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS wa_sqlite_account_acceptances (
 request_id TEXT PRIMARY KEY,
 receipt BLOB NOT NULL
);
CREATE TABLE IF NOT EXISTS wa_sqlite_artifacts (
  id TEXT PRIMARY KEY,
  updated_at INTEGER NOT NULL,
  payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS wa_sqlite_protocol_profiles (
  id TEXT PRIMARY KEY,
  updated_at INTEGER NOT NULL,
  payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS wa_sqlite_accounts (
  id TEXT PRIMARY KEY,
  e164 TEXT NOT NULL UNIQUE,
  updated_at INTEGER NOT NULL,
  payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_wa_sqlite_accounts_updated ON wa_sqlite_accounts(updated_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS wa_sqlite_message_receipts (
  wa_account_id TEXT NOT NULL REFERENCES wa_sqlite_accounts(id) ON DELETE CASCADE,
  provider_message_id TEXT NOT NULL,
  contact_ref TEXT NOT NULL,
  recipient_ref TEXT NOT NULL,
  type INTEGER NOT NULL,
  timestamp INTEGER NOT NULL,
  PRIMARY KEY (wa_account_id, provider_message_id, contact_ref, recipient_ref, type)
);
CREATE TABLE IF NOT EXISTS wa_sqlite_client_profiles (
  id TEXT PRIMARY KEY,
  wa_account_id TEXT NOT NULL,
  protocol_profile_id TEXT NOT NULL,
  status TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  payload TEXT NOT NULL,
  FOREIGN KEY(wa_account_id) REFERENCES wa_sqlite_accounts(id),
  FOREIGN KEY(protocol_profile_id) REFERENCES wa_sqlite_protocol_profiles(id)
);
CREATE INDEX IF NOT EXISTS idx_wa_sqlite_client_profiles_account ON wa_sqlite_client_profiles(wa_account_id);
CREATE TABLE IF NOT EXISTS wa_sqlite_login_states (
  id TEXT PRIMARY KEY,
  registration_id TEXT NOT NULL UNIQUE,
  wa_account_id TEXT NOT NULL,
  client_profile_id TEXT NOT NULL,
  registered_identity_id TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  payload TEXT NOT NULL,
  FOREIGN KEY(wa_account_id) REFERENCES wa_sqlite_accounts(id),
  FOREIGN KEY(client_profile_id) REFERENCES wa_sqlite_client_profiles(id)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_wa_sqlite_login_one_active ON wa_sqlite_login_states(wa_account_id) WHERE status='LOGIN_STATE_STATUS_ACTIVE';
CREATE INDEX IF NOT EXISTS idx_wa_sqlite_login_active ON wa_sqlite_login_states(wa_account_id, client_profile_id, status);
CREATE TABLE IF NOT EXISTS wa_sqlite_message_sessions (
  id TEXT PRIMARY KEY,
  wa_account_id TEXT NOT NULL,
  client_profile_id TEXT NOT NULL,
  registered_identity_id TEXT NOT NULL,
  protocol_profile_id TEXT NOT NULL,
  status TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  payload TEXT NOT NULL,
  FOREIGN KEY(wa_account_id) REFERENCES wa_sqlite_accounts(id),
  FOREIGN KEY(client_profile_id) REFERENCES wa_sqlite_client_profiles(id),
  FOREIGN KEY(protocol_profile_id) REFERENCES wa_sqlite_protocol_profiles(id)
);
CREATE INDEX IF NOT EXISTS idx_wa_sqlite_message_sessions_account ON wa_sqlite_message_sessions(wa_account_id, client_profile_id);
CREATE TABLE IF NOT EXISTS wa_sqlite_inbound_messages (
  id TEXT PRIMARY KEY,
  message_session_id TEXT NOT NULL,
  encryption_state TEXT NOT NULL,
  received_at INTEGER NOT NULL,
  payload TEXT NOT NULL,
  FOREIGN KEY(message_session_id) REFERENCES wa_sqlite_message_sessions(id)
);
CREATE INDEX IF NOT EXISTS idx_wa_sqlite_inbound_session ON wa_sqlite_inbound_messages(message_session_id, received_at);
CREATE INDEX IF NOT EXISTS idx_wa_sqlite_inbound_session_desc ON wa_sqlite_inbound_messages(message_session_id, received_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS wa_sqlite_decrypted_messages (
  id TEXT PRIMARY KEY,
  message_id TEXT NOT NULL,
  decrypted_at INTEGER NOT NULL,
  payload TEXT NOT NULL,
  FOREIGN KEY(message_id) REFERENCES wa_sqlite_inbound_messages(id)
);
CREATE INDEX IF NOT EXISTS idx_wa_sqlite_decrypted_message ON wa_sqlite_decrypted_messages(message_id);
CREATE INDEX IF NOT EXISTS idx_wa_sqlite_decrypted_message_latest ON wa_sqlite_decrypted_messages(message_id, decrypted_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS wa_sqlite_candidates (
  id TEXT PRIMARY KEY,
  message_id TEXT NOT NULL,
  decrypted_message_id TEXT NOT NULL,
  extracted_at INTEGER NOT NULL,
  payload TEXT NOT NULL,
  FOREIGN KEY(message_id) REFERENCES wa_sqlite_inbound_messages(id)
);
CREATE TABLE IF NOT EXISTS wa_sqlite_otp_messages (
  id TEXT PRIMARY KEY,
  wa_account_id TEXT NOT NULL,
  received_at INTEGER NOT NULL,
  payload TEXT NOT NULL,
  FOREIGN KEY(wa_account_id) REFERENCES wa_sqlite_accounts(id)
);
CREATE INDEX IF NOT EXISTS idx_wa_sqlite_otp_account ON wa_sqlite_otp_messages(wa_account_id, received_at DESC, id DESC);
CREATE TABLE IF NOT EXISTS wa_sqlite_contacts (
  id TEXT PRIMARY KEY,
  wa_account_id TEXT NOT NULL,
  updated_at INTEGER NOT NULL,
  payload TEXT NOT NULL,
  FOREIGN KEY(wa_account_id) REFERENCES wa_sqlite_accounts(id)
);
CREATE INDEX IF NOT EXISTS idx_wa_sqlite_contacts_account ON wa_sqlite_contacts(wa_account_id, updated_at DESC, id DESC);
`

var sqliteDeleteWAAccountStatements = []string{
	`DELETE FROM wa_sqlite_contacts WHERE wa_account_id=?`,
	`DELETE FROM wa_sqlite_otp_messages WHERE wa_account_id=?`,
	`DELETE FROM wa_sqlite_candidates WHERE message_id IN (
  SELECT m.id FROM wa_sqlite_inbound_messages m
  JOIN wa_sqlite_message_sessions s ON s.id=m.message_session_id
  WHERE s.wa_account_id=?
)`,
	`DELETE FROM wa_sqlite_decrypted_messages WHERE message_id IN (
  SELECT m.id FROM wa_sqlite_inbound_messages m
  JOIN wa_sqlite_message_sessions s ON s.id=m.message_session_id
  WHERE s.wa_account_id=?
)`,
	`DELETE FROM wa_sqlite_inbound_messages WHERE message_session_id IN (
  SELECT id FROM wa_sqlite_message_sessions WHERE wa_account_id=?
)`,
	`DELETE FROM wa_sqlite_message_sessions WHERE wa_account_id=?`,
	`DELETE FROM wa_sqlite_login_states WHERE wa_account_id=?`,
	`DELETE FROM wa_sqlite_account_credentials WHERE client_profile_id IN (
  SELECT id FROM wa_sqlite_client_profiles WHERE wa_account_id=?
)`,
	`DELETE FROM wa_sqlite_protocol_states WHERE client_profile_id IN (
  SELECT id FROM wa_sqlite_client_profiles WHERE wa_account_id=?
)`,
	`DELETE FROM wa_sqlite_client_profiles WHERE wa_account_id=?`,
	`DELETE FROM wa_sqlite_accounts WHERE id=?`,
}
