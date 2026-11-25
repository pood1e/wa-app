import { renderToStaticMarkup } from 'react-dom/server';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { describe, expect, it } from 'vitest';
import { WaEmailOtpForm } from './wa-email-otp-form';
import { WaLinkedDevices } from './wa-linked-devices';
import { WaRegistrationOtpCard } from './wa-registration-otp-card';

function clearButton(markup: string, label: string) {
  const button = markup.match(new RegExp(`<button\\b[^>]*aria-label="${label}"[^>]*>`))?.[0];
  expect(button).toBeDefined();
  expect(button).toContain('type="button"');
  return button;
}

describe('sensitive input clearing', () => {
  it.each([
    { value: '', busy: false, disabled: true },
    { value: '123', busy: false, disabled: false },
    { value: '123456', busy: true, disabled: true },
  ])('gates registration code clearing: $value / busy=$busy', ({ value, busy, disabled }) => {
    const markup = renderToStaticMarkup(<WaRegistrationOtpCard value={value} length={6} busy={busy} onChange={() => {}} onSubmit={() => {}} />);
    expect(clearButton(markup, '清空验证码')?.includes('disabled=""')).toBe(disabled);
    expect(markup).toContain('aria-label="短信验证码"');
  });

  it.each([
    { code: '', busy: false, disabled: true },
    { code: '123', busy: false, disabled: false },
    { code: '123456', busy: true, disabled: true },
  ])('gates email code clearing: $code / busy=$busy', ({ code, busy, disabled }) => {
    const markup = renderToStaticMarkup(<WaEmailOtpForm code={code} busy={busy} onCodeChange={() => {}} onRequest={() => {}} onVerify={() => {}} />);
    expect(clearButton(markup, '清空验证码')?.includes('disabled=""')).toBe(disabled);
  });

  it('starts with QR clearing disabled and keeps authorization explicit', () => {
    const client = new QueryClient();
    const markup = renderToStaticMarkup(<QueryClientProvider client={client}><WaLinkedDevices accountID="fixture" online={false} onDone={() => {}} onError={() => {}} /></QueryClientProvider>);
    expect(clearButton(markup, '清空二维码')).toContain('disabled=""');
    expect(markup).toContain('WhatsApp Web 二维码内容');
    expect(client.getMutationCache().getAll()).toHaveLength(0);
    client.clear();
  });
});
