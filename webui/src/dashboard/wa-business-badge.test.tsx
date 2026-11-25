import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { WAAccountType } from '../proto/byte/v/forge/waapp/model/v1/account_type';
import { WaBusinessBadge } from './wa-business-badge';

describe('business account badge', () => {
  it('labels business accounts without implying verification', () => {
    const markup = renderToStaticMarkup(<WaBusinessBadge accountType={WAAccountType.WA_ACCOUNT_TYPE_BUSINESS} />);
    expect(markup).toContain('WhatsApp Business 商业账号');
    expect(markup).toContain('>B</span>商业账号');
    expect(markup).not.toContain('认证');
  });

  it('keeps the compact avatar marker accessible', () => {
    const markup = renderToStaticMarkup(<WaBusinessBadge accountType={WAAccountType.WA_ACCOUNT_TYPE_BUSINESS} compact />);
    expect(markup).toContain('aria-label="WhatsApp Business 商业账号"');
    expect(markup).toContain('>B</span></span>');
  });

  it.each([WAAccountType.WA_ACCOUNT_TYPE_PERSONAL, WAAccountType.UNRECOGNIZED])('omits the marker for other account types (%s)', (accountType) => {
    expect(renderToStaticMarkup(<WaBusinessBadge accountType={accountType} />)).toBe('');
  });
});
