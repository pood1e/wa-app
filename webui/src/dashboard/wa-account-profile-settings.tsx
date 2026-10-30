import { type FormEvent, type RefObject, useEffect, useRef, useState } from 'react';
import AvatarEditor, { type AvatarEditorRef } from 'react-avatar-editor';
import { Camera, Check, Loader2, Pencil, UserRound, X } from 'lucide-react';
import { useMutation, useQueryClient } from '@tanstack/react-query';
import type { WAAccount } from '../proto/byte/v/forge/waapp/v1/profile';
import { setWaAccountProfileName, setWaAccountProfilePicture, waAccountID, waAccountProfilePictureURL } from './wa-api';
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar';
import { WaAvatarImage } from './wa-avatar-image';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';

const maxProfilePictureBytes = 2 * 1024 * 1024;

type Props = {
  account: WAAccount;
  onDone: (message: string) => void;
  onError: (message: string) => void;
  onAccountChanged: () => void;
  onAvatarChanged: () => void;
};

export function WaAccountProfileSettings({ account, onDone, onError, onAccountChanged, onAvatarChanged }: Props) {
  const queryClient = useQueryClient();
  const savedName = (account.display_name || '').trim();
  const [currentName, setCurrentName] = useState(savedName);
  const [displayName, setDisplayName] = useState(savedName);
  const [nameEditing, setNameEditing] = useState(!savedName);
  const [picture, setPicture] = useState<File | null>(null);
  const [activePicture, setActivePicture] = useState('');
  const [avatarVersion, setAvatarVersion] = useState('');
  const fileInput = useRef<HTMLInputElement>(null);
  const editor = useRef<AvatarEditorRef>(null);
  const resetPictureSelection = () => {
    setPicture(null);
    if (fileInput.current) fileInput.current.value = '';
  };
  const handleError = (error: unknown) => onError(error instanceof Error ? error.message : String(error));
  const nameMutation = useMutation({
    mutationFn: () => {
      const name = displayName.trim();
      if (!name) throw new Error('账号名称不能为空');
      if ([...name].length > 25) throw new Error('账号名称不能超过 25 个字符');
      return setWaAccountProfileName(account, name);
    },
    onSuccess: () => {
      const nextName = displayName.trim();
      setCurrentName(nextName);
      setDisplayName(nextName);
      setNameEditing(false);
      onAccountChanged();
      onDone(currentName ? '名称已修改' : '名称已设置');
    },
    onError: handleError,
  });
  const pictureMutation = useMutation({
    mutationFn: async ({ dataURL, file }: { dataURL: string; file: File }) => {
      if (file.size > maxProfilePictureBytes) throw new Error('头像图片不能超过 2 MiB');
      const response = await setWaAccountProfilePicture(account, { image_base64: dataURLBase64(dataURL), content_type: 'image/jpeg' });
      return { dataURL, response };
    },
    onSuccess: ({ dataURL, response }) => {
      setActivePicture(dataURL);
      setAvatarVersion(String(Date.now()));
      resetPictureSelection();
      const path = waAccountProfilePictureURL(account).split('?')[0];
      void queryClient.invalidateQueries({ predicate: (query) => query.queryKey[0] === 'wa' && query.queryKey[1] === 'picture' && typeof query.queryKey[2] === 'string' && query.queryKey[2].split('?')[0] === path });
      onAvatarChanged();
      onDone(response.profile_picture_id ? '头像已提交' : '头像请求已提交');
    },
    onError: (error) => { resetPictureSelection(); handleError(error); },
  });
  const accountID = waAccountID(account);
  const remoteAvatar = waAccountProfilePictureURL(account, avatarVersion || account.audit?.updated_at || 'latest');
  const pictureBusy = pictureMutation.isPending;
  const name = displayName.trim();
  const nameBusy = nameMutation.isPending;
  const nameChanged = name !== currentName;
  const nameAction = currentName ? '修改名称' : '设置名称';
  useEffect(() => {
    setCurrentName(savedName);
    setDisplayName(savedName);
    setNameEditing(!savedName);
    setActivePicture('');
    setAvatarVersion('');
    setPicture(null);
    if (fileInput.current) fileInput.current.value = '';
  }, [accountID, savedName]);
  return (
    <section className="grid gap-6">
      <div className="grid justify-items-center gap-6">
        <Button className="group/avatar relative size-24 shrink-0 overflow-hidden rounded-full bg-muted p-0 hover:bg-muted disabled:cursor-not-allowed disabled:opacity-70" variant="ghost" type="button" disabled={pictureBusy} title="更换头像" aria-label="更换头像" onClick={() => { if (fileInput.current) fileInput.current.value = ''; fileInput.current?.click(); }}>
          {picture ? <AvatarPreview editor={editor} image={picture} onReady={(dataURL) => pictureMutation.mutate({ dataURL, file: picture })} onError={(message) => { resetPictureSelection(); onError(message); }} /> : <StoredAvatar src={remoteAvatar} preview={activePicture} />}
          {pictureBusy ? <span className="absolute inset-0 grid place-items-center bg-background/70"><Loader2 className="size-4 animate-spin" /></span> : null}
          {!pictureBusy && !picture ? <span className="absolute inset-0 grid place-items-center bg-black/40 text-white opacity-0 transition-opacity group-hover/avatar:opacity-100 group-focus-visible/avatar:opacity-100"><Camera className="size-6" /></span> : null}
        </Button>
        {currentName && !nameEditing ? (
          <div className="flex w-full min-w-0 items-center gap-2 border-b border-border pb-3">
            <div className="min-w-0 flex-1"><p className="mb-2 text-xs text-primary">你的名字</p><p className="truncate text-base">{currentName}</p></div>
            <Button className="rounded-full text-muted-foreground" size="icon" variant="ghost" type="button" title="修改名称" aria-label="修改名称" onClick={() => { setDisplayName(currentName); setNameEditing(true); }}><Pencil size={16} /></Button>
          </div>
        ) : (
          <form className="flex w-full min-w-0 items-center gap-2" onSubmit={(event) => submitName(event, () => nameMutation.mutate())}>
            <Input className="min-w-0 flex-1 rounded-none border-0 border-b border-input bg-transparent px-0 shadow-none focus-visible:ring-0 focus-visible:border-primary dark:bg-transparent" value={displayName} maxLength={25} placeholder="你的名字" aria-label="账号名称" disabled={nameBusy} onChange={(event) => setDisplayName(event.target.value)} />
            {currentName ? <Button className="h-10 w-10 px-0" variant="ghost" type="button" disabled={nameBusy} title="取消修改" aria-label="取消修改" onClick={() => { setDisplayName(currentName); setNameEditing(false); }}><X size={16} /></Button> : null}
            <Button className="h-10 w-10 px-0" type="submit" disabled={nameBusy || !name || !nameChanged || [...name].length > 25} title={nameAction} aria-label={nameAction}>
              {nameBusy ? <Loader2 className="size-4 animate-spin" /> : <Check size={16} />}
            </Button>
          </form>
        )}
      </div>
      <p className="text-xs leading-5 text-muted-foreground">此名称会向你的 WhatsApp 联系人显示。</p>
      <Input ref={fileInput} className="hidden" type="file" accept="image/jpeg,image/png,image/webp" disabled={pictureBusy} onChange={(event) => setSelectedPicture(event.target.files?.[0] || null, setPicture, onError)} />
    </section>
  );
}

function submitName(event: FormEvent<HTMLFormElement>, run: () => void) {
  event.preventDefault();
  run();
}

function StoredAvatar({ src, preview }: { src: string; preview: string }) {
  return (
    <Avatar className="size-24">
      {preview ? <AvatarImage src={preview} alt="当前头像" /> : src ? <WaAvatarImage key={src} src={src} alt="当前头像" /> : null}
      <AvatarFallback className="bg-muted text-muted-foreground">
        <UserRound className="size-12" strokeWidth={1.4} />
      </AvatarFallback>
    </Avatar>
  );
}

function AvatarPreview({ editor, image, onReady, onError }: { editor: RefObject<AvatarEditorRef | null>; image: File; onReady: (dataURL: string) => void; onError: (message: string) => void }) {
  const preparedImage = useRef<File | null>(null);
  return (
    <AvatarEditor
      ref={editor}
      image={image}
      width={512}
      height={512}
      border={0}
      borderRadius={256}
      scale={1}
      backgroundColor="#ffffff"
      onLoadSuccess={() => {
        if (preparedImage.current === image) return;
        preparedImage.current = image;
        onReady(avatarDataURL(editor.current));
      }}
      onLoadFailure={() => onError('头像图片加载失败')}
      style={{ width: '6rem', height: '6rem' }}
    />
  );
}

function setSelectedPicture(file: File | null, setPicture: (file: File | null) => void, onError: (message: string) => void) {
  if (file && file.size > maxProfilePictureBytes) {
    onError('头像图片不能超过 2 MiB');
    return;
  }
  setPicture(file);
}

function avatarDataURL(editor: AvatarEditorRef | null) {
  const dataURL = editor?.getImageScaledToCanvas().toDataURL('image/jpeg', 0.92);
  if (!dataURL) throw new Error('头像图片编码失败');
  return dataURL;
}

function dataURLBase64(dataURL: string) {
  return dataURL.slice(dataURL.indexOf(',') + 1);
}
