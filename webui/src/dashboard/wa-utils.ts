import { parsePhoneNumberFromString } from 'libphonenumber-js';
import type { PhoneTarget } from '../proto/byte/v/forge/waapp/v1/common';

const PHONE_NOT_POSSIBLE_MESSAGE = '手机号位数不符合国家规则，请检查国家拨号码和手机号。';

export type WaResolvedPhone = {
  e164: string;
  input: PhoneTarget;
};

export type WaPhoneResolveResult = { target: WaResolvedPhone | null; error?: string };

export function resolveWaPhoneTarget(value: string, countryCallingCode = ''): WaPhoneResolveResult {
  const raw = value.trim();
  const digits = value.replace(/\D+/g, '');
  if (!digits) return { target: null, error: '请输入手机号' };
  const callingCode = countryCallingCode.replace(/\D+/g, '');
  if (!callingCode) return { target: null, error: '请输入国家拨号码，例如 992。' };
  const parseInput = raw.startsWith('+') ? raw : internationalNumberFromCallingCode(digits, callingCode);
  const phone = parsePhoneNumberFromString(parseInput);
  if (!phone?.countryCallingCode || !phone.nationalNumber) {
    return { target: null, error: '手机号无法解析，请输入手机号和国家拨号码。' };
  }
  if (phone.countryCallingCode !== callingCode) {
    return { target: null, error: `手机号与拨号码不一致：号码为 +${phone.countryCallingCode}，拨号码为 +${callingCode}。` };
  }
  if (!phone.isPossible()) {
    return { target: null, error: PHONE_NOT_POSSIBLE_MESSAGE };
  }
  const e164 = phone.number;
  const countryCode = phone.country || '';
  return {
    target: {
      e164,
      input: {
        national_number: phone.nationalNumber,
        e164_number: e164,
        country_calling_code: phone.countryCallingCode,
        country_iso2: countryCode
      }
    }
  };
}

function internationalNumberFromCallingCode(digits: string, callingCode: string) {
  if (!callingCode) return '';
  return `+${digits.startsWith(callingCode) ? digits : `${callingCode}${digits}`}`;
}
