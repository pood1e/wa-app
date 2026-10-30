import type { WAContact } from '../proto/byte/v/forge/waapp/v1/contacts';
import type { GroupChat } from '../proto/byte/v/forge/waapp/v1/groups';
import { canonicalContactRef } from './wa-contact-aliases';
import { contactProfilePictureURL } from './wa-contact-picture';

type SenderName = { name: string; priority: number };

export function waContactName(record: WAContact) {
  return contactName(record)?.name || '';
}

export function createWaSenderLookup(records: WAContact[], group?: GroupChat) {
  const names = new Map<string, SenderName>();
  const phones = new Map<string, string>();
  const pictures = new Map<string, WAContact>();
  for (const member of group?.members || []) {
    const phone = phoneNumber(member.phone_ref);
    if (phone) phones.set(canonicalContactRef(member.member_ref), phone);
  }
  for (const record of records) {
    const id = canonicalContactRef(record.jid || '');
    if (id.endsWith('@g.us')) continue;
    const phone = phoneNumber(record.number) || phoneNumber(record.jid);
    if (phone) phones.set(id, phone);
    if (record.contact_id) {
      for (const ref of [id, phone ? `${phone}@s.whatsapp.net` : '']) {
        const previous = pictures.get(ref);
        if (ref && (!previous || (!previous.profile_picture_id && record.profile_picture_id))) pictures.set(ref, record);
      }
    }
    const name = contactName(record);
    if (!name) continue;
    for (const ref of [id, phone ? `${phone}@s.whatsapp.net` : '']) {
      if (ref && (!names.has(ref) || name.priority > names.get(ref)!.priority)) names.set(ref, name);
    }
  }
  return (ref: string, isGroup: boolean) => {
    const id = canonicalContactRef(ref);
    const phone = phones.get(id) || phoneNumber(id);
    const direct = names.get(id);
    const alias = phone ? names.get(`${phone}@s.whatsapp.net`) : undefined;
    const name = alias && (!direct || alias.priority > direct.priority) ? alias.name : direct?.name;
    const directPicture = pictures.get(id);
    const phonePicture = phone ? pictures.get(`${phone}@s.whatsapp.net`) : undefined;
    const picture = phonePicture?.profile_picture_id && !directPicture?.profile_picture_id ? phonePicture : directPicture || phonePicture;
    return {
      name: name || (phone ? `+${phone}` : isGroup ? (id.endsWith('@lid') ? `成员 ${id.split('@')[0]}` : '未知成员') : ''),
      detail: name && phone ? `+${phone}` : undefined,
      pictureURL: picture ? contactProfilePictureURL(picture) : undefined,
    };
  };
}

function contactName(record: WAContact): SenderName | undefined {
  for (const [value, priority] of [[record.display_name, 3], [record.verified_name, 2], [record.wa_name, 1]] as const) {
    const name = (value || '').trim();
    if (!name || name === '0' || name === '未知联系人' || /^(?:LID |企业账号 |联系人 )/.test(name) || /^\+?\d{6,}$/.test(name)) continue;
    return { name, priority };
  }
}

function phoneNumber(value?: string) {
  const ref = canonicalContactRef(value || '');
  const number = ref.endsWith('@s.whatsapp.net') ? ref.split('@')[0] : ref;
  return /^\d+$/.test(number) ? number : '';
}
