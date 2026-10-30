import { REGEXP_ONLY_DIGITS } from 'input-otp';
import { CheckCircle2, KeyRound } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardTitle } from '@/components/ui/card';
import { InputOTP, InputOTPGroup, InputOTPSlot } from '@/components/ui/input-otp';
import { VerificationDeliveryMethod as Method } from '../proto/byte/v/forge/waapp/v1/registration';

type Props = {
  value: string;
  length: number;
  method?: Method;
  busy?: boolean;
  onChange: (value: string) => void;
  onSubmit: () => void;
};

export function WaRegistrationOtpCard({ value, length, method, busy, onChange, onSubmit }: Props) {
  const [label, hint] = verificationInstructions(method);
  return (
    <Card className="border-dashed">
      <CardContent className="grid gap-2 p-3">
        <CardTitle className="inline-flex items-center gap-2 text-sm"><KeyRound size={15} />{label}</CardTitle>
        <p className="text-xs text-muted-foreground">{hint}</p>
        <div className="flex items-center gap-2">
          <InputOTP maxLength={length} value={value} onChange={onChange} pattern={REGEXP_ONLY_DIGITS} inputMode="numeric" autoComplete="one-time-code" disabled={busy}>
            <InputOTPGroup>
              {Array.from({ length }, (_, index) => <InputOTPSlot key={index} index={index} />)}
            </InputOTPGroup>
          </InputOTP>
          <Button type="button" size="icon" disabled={busy || value.trim().length !== length} title="提交 OTP" aria-label="提交 OTP" onClick={onSubmit}><CheckCircle2 size={14} /></Button>
        </div>
      </CardContent>
    </Card>
  );
}

function verificationInstructions(method?: Method) {
  switch (method) {
    case Method.VERIFICATION_DELIVERY_METHOD_WA_OLD: return ['旧设备验证码', '请在旧设备上的 WhatsApp 查看本次请求的验证码。'];
    case Method.VERIFICATION_DELIVERY_METHOD_EMAIL_OTP: return ['邮箱验证码', '请输入本次验证邮件中的新验证码。'];
    case Method.VERIFICATION_DELIVERY_METHOD_VOICE: return ['语音验证码', '请输入本次语音播报的新验证码。'];
    default: return ['短信验证码', '请输入本次短信中的新验证码。'];
  }
}
