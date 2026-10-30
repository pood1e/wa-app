CREATE TABLE IF NOT EXISTS wa_app_artifacts (
  artifact_id TEXT PRIMARY KEY,
  label TEXT NOT NULL,
  version_label TEXT NOT NULL DEFAULT '',
  sha256 TEXT NOT NULL DEFAULT '',
  observed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_protocol_profiles (
  protocol_profile_id TEXT PRIMARY KEY,
  app_artifact_id TEXT NOT NULL REFERENCES wa_app_artifacts(artifact_id),
  display_name TEXT NOT NULL,
  app_version TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  capabilities TEXT[] NOT NULL DEFAULT '{}',
  registration_flows TEXT[] NOT NULL DEFAULT '{}',
  message_transports TEXT[] NOT NULL DEFAULT '{}',
  discovered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_accounts (
  wa_account_id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL DEFAULT '',
  e164_number TEXT NOT NULL UNIQUE,
  country_calling_code TEXT NOT NULL DEFAULT '',
  national_number TEXT NOT NULL DEFAULT '',
  country_iso2 TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  two_factor_auth_configured BOOLEAN,
  two_factor_email_configured BOOLEAN,
  two_factor_email_address TEXT,
  two_factor_email_verified BOOLEAN,
  two_factor_email_confirmed BOOLEAN,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_account_proxies (
  wa_account_id TEXT PRIMARY KEY REFERENCES wa_accounts(wa_account_id) ON DELETE CASCADE,
  configuration BYTEA NOT NULL
);

CREATE TABLE IF NOT EXISTS wa_client_profiles (
  client_profile_id TEXT PRIMARY KEY,
  wa_account_id TEXT NOT NULL REFERENCES wa_accounts(wa_account_id),
  protocol_profile_id TEXT NOT NULL REFERENCES wa_protocol_profiles(protocol_profile_id),
  status TEXT NOT NULL,
  registration_key_state TEXT NOT NULL,
  messaging_key_state TEXT NOT NULL,
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error_message TEXT NOT NULL DEFAULT '',
  last_error_retryable BOOLEAN NOT NULL DEFAULT false,
  last_used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_login_states (
  login_state_id TEXT PRIMARY KEY,
  registration_id TEXT NOT NULL,
  wa_account_id TEXT NOT NULL REFERENCES wa_accounts(wa_account_id),
  client_profile_id TEXT NOT NULL REFERENCES wa_client_profiles(client_profile_id),
  registered_identity_id TEXT NOT NULL,
  service_account_id TEXT NOT NULL DEFAULT '',
  service_login_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error_message TEXT NOT NULL DEFAULT '',
  last_error_retryable BOOLEAN NOT NULL DEFAULT false,
  registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_verified_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (registration_id),
  UNIQUE (registered_identity_id)
);

CREATE TABLE IF NOT EXISTS wa_message_sessions (
  message_session_id TEXT PRIMARY KEY,
  wa_account_id TEXT NOT NULL REFERENCES wa_accounts(wa_account_id),
  client_profile_id TEXT NOT NULL REFERENCES wa_client_profiles(client_profile_id),
  registered_identity_id TEXT NOT NULL,
  protocol_profile_id TEXT NOT NULL REFERENCES wa_protocol_profiles(protocol_profile_id),
  status TEXT NOT NULL,
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error_message TEXT NOT NULL DEFAULT '',
  last_error_retryable BOOLEAN NOT NULL DEFAULT false,
  opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_seen_at TIMESTAMPTZ,
  closed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS wa_inbound_messages (
  message_id TEXT PRIMARY KEY,
  message_session_id TEXT NOT NULL REFERENCES wa_message_sessions(message_session_id),
  kind TEXT NOT NULL,
  encryption_state TEXT NOT NULL,
  ack_status TEXT NOT NULL,
  contact_ref TEXT NOT NULL DEFAULT '',
  sender_ref TEXT NOT NULL DEFAULT '',
  payload_ref TEXT NOT NULL DEFAULT '',
  provider_message_id TEXT NOT NULL DEFAULT '',
  provider_timestamp TIMESTAMPTZ,
  read_at TIMESTAMPTZ,
  delete_status TEXT NOT NULL DEFAULT 'MESSAGE_DELETE_STATUS_NOT_DELETED',
  deleted_at TIMESTAMPTZ,
  direction TEXT NOT NULL DEFAULT 'ACCOUNT_MESSAGE_DIRECTION_INBOUND',
  source TEXT NOT NULL DEFAULT 'ACCOUNT_MESSAGE_SOURCE_LONG_CONNECTION',
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error_message TEXT NOT NULL DEFAULT '',
  last_error_retryable BOOLEAN NOT NULL DEFAULT false,
  received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_decrypted_messages (
  decrypted_message_id TEXT PRIMARY KEY,
  message_id TEXT NOT NULL REFERENCES wa_inbound_messages(message_id),
  status TEXT NOT NULL,
  plaintext_ref TEXT NOT NULL DEFAULT '',
  plaintext_value TEXT NOT NULL DEFAULT '',
  plaintext_redacted TEXT NOT NULL DEFAULT '',
  plaintext_secret_ref TEXT NOT NULL DEFAULT '',
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error_message TEXT NOT NULL DEFAULT '',
  last_error_retryable BOOLEAN NOT NULL DEFAULT false,
  decrypted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_extracted_candidates (
  candidate_id TEXT PRIMARY KEY,
  message_id TEXT NOT NULL REFERENCES wa_inbound_messages(message_id),
  decrypted_message_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  redacted_value TEXT NOT NULL DEFAULT '',
  secret_ref TEXT NOT NULL DEFAULT '',
  confidence DOUBLE PRECISION NOT NULL DEFAULT 0,
  extracted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_otp_messages (
  otp_message_id TEXT PRIMARY KEY,
  wa_account_id TEXT NOT NULL REFERENCES wa_accounts(wa_account_id),
  client_profile_id TEXT NOT NULL DEFAULT '',
  registered_identity_id TEXT NOT NULL DEFAULT '',
  message_id TEXT NOT NULL DEFAULT '',
  candidate_id TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL,
  source_party TEXT NOT NULL DEFAULT '',
  otp_value TEXT NOT NULL,
  otp_redacted TEXT NOT NULL DEFAULT '',
  otp_secret_ref TEXT NOT NULL DEFAULT '',
  received_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_contacts (
  contact_id TEXT PRIMARY KEY,
  wa_account_id TEXT NOT NULL REFERENCES wa_accounts(wa_account_id) ON DELETE CASCADE,
  jid TEXT NOT NULL DEFAULT '',
  number TEXT NOT NULL DEFAULT '',
  display_name TEXT NOT NULL DEFAULT '',
  wa_name TEXT NOT NULL DEFAULT '',
  verified_name TEXT NOT NULL DEFAULT '',
  profile_picture_id TEXT NOT NULL DEFAULT '',
  kind TEXT NOT NULL,
  is_whatsapp_user BOOLEAN NOT NULL DEFAULT false,
  is_reachable BOOLEAN NOT NULL DEFAULT false,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS wa_accounts_e164_number_key ON wa_accounts (e164_number);
CREATE UNIQUE INDEX IF NOT EXISTS wa_login_states_registration_id_key ON wa_login_states (registration_id);
CREATE UNIQUE INDEX IF NOT EXISTS wa_login_states_registered_identity_id_key ON wa_login_states (registered_identity_id);
CREATE INDEX IF NOT EXISTS wa_inbound_messages_session_received_idx ON wa_inbound_messages (message_session_id, received_at DESC, message_id DESC);
CREATE INDEX IF NOT EXISTS wa_inbound_messages_contact_received_idx ON wa_inbound_messages (message_session_id, contact_ref, received_at DESC, message_id DESC);
CREATE INDEX IF NOT EXISTS wa_inbound_messages_thread_received_idx ON wa_inbound_messages ((COALESCE(NULLIF(contact_ref,''), sender_ref)), received_at DESC, message_id DESC);
CREATE INDEX IF NOT EXISTS wa_inbound_messages_provider_message_idx ON wa_inbound_messages (message_session_id, provider_message_id);
CREATE INDEX IF NOT EXISTS wa_inbound_messages_delete_status_idx ON wa_inbound_messages (message_session_id, delete_status, received_at DESC);
CREATE INDEX IF NOT EXISTS wa_decrypted_messages_message_decrypted_idx ON wa_decrypted_messages (message_id, decrypted_at DESC, decrypted_message_id DESC);
CREATE INDEX IF NOT EXISTS wa_contacts_account_updated_idx ON wa_contacts (wa_account_id, updated_at DESC, contact_id DESC);
CREATE INDEX IF NOT EXISTS wa_contacts_account_jid_idx ON wa_contacts (wa_account_id, jid);

CREATE TABLE IF NOT EXISTS wa_account_credentials (
 client_profile_id TEXT PRIMARY KEY REFERENCES wa_client_profiles(client_profile_id) ON DELETE CASCADE,
 credentials BYTEA NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS wa_protocol_states (
 client_profile_id TEXT PRIMARY KEY REFERENCES wa_client_profiles(client_profile_id) ON DELETE CASCADE,
 recovery BYTEA NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS wa_account_acceptances (
 request_id TEXT PRIMARY KEY,
 wa_account_id TEXT NOT NULL,
 receipt BYTEA NOT NULL,
 accepted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS wa_login_states_one_active_account_idx
 ON wa_login_states (wa_account_id) WHERE status='LOGIN_STATE_STATUS_ACTIVE';
CREATE TABLE IF NOT EXISTS wa_message_receipts (
  wa_account_id TEXT NOT NULL REFERENCES wa_accounts(wa_account_id) ON DELETE CASCADE,
  provider_message_id TEXT NOT NULL,
  contact_ref TEXT NOT NULL,
  recipient_ref TEXT NOT NULL,
  type INTEGER NOT NULL,
  timestamp TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (wa_account_id, provider_message_id, contact_ref, recipient_ref, type)
);

ALTER TABLE wa_inbound_messages ADD COLUMN IF NOT EXISTS read_receipt_sent_at TIMESTAMPTZ;

ALTER TABLE wa_inbound_messages ADD COLUMN IF NOT EXISTS delivery_receipt_sent_at TIMESTAMPTZ;

ALTER TABLE wa_decrypted_messages ADD COLUMN IF NOT EXISTS content JSONB NOT NULL DEFAULT '{}'::jsonb;
CREATE INDEX IF NOT EXISTS wa_decrypted_messages_change_target_idx ON wa_decrypted_messages ((content->'change'->>'target_provider_message_id'));

ALTER TABLE wa_contacts ADD COLUMN IF NOT EXISTS saved_name TEXT NOT NULL DEFAULT '';
