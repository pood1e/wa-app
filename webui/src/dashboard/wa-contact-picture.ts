import { WAContactKind, type WAContact } from '../proto/byte/v/forge/waapp/v1/contacts';

export function contactProfilePictureURL(record: WAContact) {
  if (!record.contact_id || record.kind === WAContactKind.WA_CONTACT_KIND_SYSTEM) return undefined;
  const version = record.profile_picture_id || 'latest';
  return `/api/wa/contacts/${encodeURIComponent(record.contact_id)}/profile-picture?v=${encodeURIComponent(version)}`;
}
