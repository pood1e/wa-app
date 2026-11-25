import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';
import { ClientPlatform } from '../proto/byte/v/forge/waapp/protocol/v1/account';
import { WaPlatformChoice } from './wa-platform-choice';

describe('native registration platform choice', () => {
  it.each([ClientPlatform.CLIENT_PLATFORM_ANDROID, ClientPlatform.CLIENT_PLATFORM_IOS])('offers both platforms with one selected (%s)', (platform) => {
    const markup = renderToStaticMarkup(<WaPlatformChoice platform={platform} onChange={() => {}} />);
    expect(markup).toContain('aria-label="协议平台"');
    expect(markup).toContain('>Android</button>');
    expect(markup).toContain('>iOS</button>');
    expect(markup.match(/aria-pressed="true"/g)).toHaveLength(1);
  });

  it('locks platform changes while an operation is pending', () => {
    const markup = renderToStaticMarkup(<WaPlatformChoice platform={ClientPlatform.CLIENT_PLATFORM_IOS} disabled onChange={() => {}} />);
    expect(markup.match(/disabled=""/g)).toHaveLength(2);
  });
});
