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

ALTER TABLE wa_accounts ADD COLUMN IF NOT EXISTS display_name TEXT NOT NULL DEFAULT '';
ALTER TABLE wa_accounts ADD COLUMN IF NOT EXISTS two_factor_auth_configured BOOLEAN;
ALTER TABLE wa_accounts ADD COLUMN IF NOT EXISTS two_factor_email_configured BOOLEAN;
ALTER TABLE wa_accounts ADD COLUMN IF NOT EXISTS two_factor_email_address TEXT;
ALTER TABLE wa_accounts ADD COLUMN IF NOT EXISTS two_factor_email_verified BOOLEAN;
ALTER TABLE wa_accounts ADD COLUMN IF NOT EXISTS two_factor_email_confirmed BOOLEAN;
ALTER TABLE wa_accounts DROP COLUMN IF EXISTS proxy_policy_json;

CREATE TABLE IF NOT EXISTS wa_client_profiles (
  client_profile_id TEXT PRIMARY KEY,
  wa_account_id TEXT NOT NULL REFERENCES wa_accounts(wa_account_id),
  protocol_profile_id TEXT NOT NULL REFERENCES wa_protocol_profiles(protocol_profile_id),
  status TEXT NOT NULL,
  registration_key_state TEXT NOT NULL,
  messaging_key_state TEXT NOT NULL,
  state_ref TEXT NOT NULL DEFAULT '',
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error_message TEXT NOT NULL DEFAULT '',
  last_error_retryable BOOLEAN NOT NULL DEFAULT false,
  last_used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_client_profile_states (
  client_profile_id TEXT PRIMARY KEY REFERENCES wa_client_profiles(client_profile_id) ON DELETE CASCADE,
  state_json JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_account_probes (
  account_probe_id TEXT PRIMARY KEY,
  wa_account_id TEXT NOT NULL REFERENCES wa_accounts(wa_account_id),
  client_profile_id TEXT NOT NULL REFERENCES wa_client_profiles(client_profile_id),
  status TEXT NOT NULL,
  supported_methods TEXT[] NOT NULL DEFAULT '{}',
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error_message TEXT NOT NULL DEFAULT '',
  last_error_retryable BOOLEAN NOT NULL DEFAULT false,
  probed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS wa_verification_requests (
  verification_request_id TEXT PRIMARY KEY,
  wa_account_id TEXT NOT NULL REFERENCES wa_accounts(wa_account_id),
  client_profile_id TEXT NOT NULL REFERENCES wa_client_profiles(client_profile_id),
  delivery_method TEXT NOT NULL,
  status TEXT NOT NULL,
  expected_code_length INTEGER NOT NULL DEFAULT 0,
  retry_after_seconds INTEGER NOT NULL DEFAULT 0,
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error_message TEXT NOT NULL DEFAULT '',
  last_error_retryable BOOLEAN NOT NULL DEFAULT false,
  requested_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ
);

ALTER TABLE wa_verification_requests ADD COLUMN IF NOT EXISTS retry_after_seconds INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS wa_registrations (
  registration_id TEXT PRIMARY KEY,
  verification_request_id TEXT NOT NULL REFERENCES wa_verification_requests(verification_request_id),
  wa_account_id TEXT NOT NULL REFERENCES wa_accounts(wa_account_id),
  client_profile_id TEXT NOT NULL REFERENCES wa_client_profiles(client_profile_id),
  status TEXT NOT NULL,
  registered_identity_id TEXT NOT NULL DEFAULT '',
  service_account_id TEXT NOT NULL DEFAULT '',
  service_login_id TEXT NOT NULL DEFAULT '',
  last_error_code TEXT NOT NULL DEFAULT '',
  last_error_message TEXT NOT NULL DEFAULT '',
  last_error_retryable BOOLEAN NOT NULL DEFAULT false,
  submitted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  completed_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS wa_login_states (
  login_state_id TEXT PRIMARY KEY,
  registration_id TEXT NOT NULL REFERENCES wa_registrations(registration_id),
  wa_account_id TEXT NOT NULL REFERENCES wa_accounts(wa_account_id),
  client_profile_id TEXT NOT NULL REFERENCES wa_client_profiles(client_profile_id),
  registered_identity_id TEXT NOT NULL,
  service_account_id TEXT NOT NULL DEFAULT '',
  service_login_id TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL,
  state_ref TEXT NOT NULL DEFAULT '',
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

ALTER TABLE wa_inbound_messages ADD COLUMN IF NOT EXISTS direction TEXT NOT NULL DEFAULT 'ACCOUNT_MESSAGE_DIRECTION_INBOUND';
ALTER TABLE wa_inbound_messages ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'ACCOUNT_MESSAGE_SOURCE_LONG_CONNECTION';

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

DO $$
DECLARE
  wa_table_name TEXT;
BEGIN
  FOREACH wa_table_name IN ARRAY ARRAY[
    'wa_app_artifacts',
    'wa_protocol_profiles',
    'wa_accounts',
    'wa_client_profiles',
    'wa_client_profile_states',
    'wa_account_probes',
    'wa_verification_requests',
    'wa_registrations',
    'wa_login_states',
    'wa_message_sessions',
    'wa_inbound_messages',
    'wa_decrypted_messages',
    'wa_extracted_candidates',
    'wa_otp_messages',
    'wa_contacts'
  ] LOOP
    IF to_regclass(wa_table_name) IS NOT NULL THEN
      EXECUTE format('ALTER TABLE %I DROP COLUMN IF EXISTS workspace_id CASCADE', wa_table_name);
    END IF;
  END LOOP;
END $$;

ALTER TABLE wa_decrypted_messages ADD COLUMN IF NOT EXISTS plaintext_value TEXT NOT NULL DEFAULT '';
ALTER TABLE wa_inbound_messages ADD COLUMN IF NOT EXISTS contact_ref TEXT NOT NULL DEFAULT '';
ALTER TABLE wa_inbound_messages ADD COLUMN IF NOT EXISTS provider_message_id TEXT NOT NULL DEFAULT '';
ALTER TABLE wa_inbound_messages ADD COLUMN IF NOT EXISTS provider_timestamp TIMESTAMPTZ;
ALTER TABLE wa_inbound_messages ADD COLUMN IF NOT EXISTS read_at TIMESTAMPTZ;
ALTER TABLE wa_inbound_messages ADD COLUMN IF NOT EXISTS delete_status TEXT NOT NULL DEFAULT 'MESSAGE_DELETE_STATUS_NOT_DELETED';
ALTER TABLE wa_inbound_messages ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE wa_otp_messages ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;
ALTER TABLE wa_contacts ADD COLUMN IF NOT EXISTS profile_picture_id TEXT NOT NULL DEFAULT '';

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
