import { describe, expect, it } from 'vitest';
import { isAccountPassword } from './wa-account-security-model';

describe('native account password requirements', () => {
  it.each([
    ['012345', false],
    ['Abcdefg1', true],
    ['Abcdefghijklmnopqrs1', true],
    ['Abcdefghijklmnopqrst1', false],
    ['abcdefgh', false],
    ['12345678', false],
    ['密碼測試１２３４', true],
    [' Password1 ', true],
    ['😀😀😀😀😀😀😀😀😀a1', true],
    ['😀😀😀😀😀😀😀😀😀😀a1', false],
    ['𐐀1234567', false],
  ])('validates native UTF-16 length and characters', (value, valid) => {
    expect(isAccountPassword(value)).toBe(valid);
  });
});
