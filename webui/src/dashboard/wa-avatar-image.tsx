import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { AvatarImage } from '@/components/ui/avatar';
import { avatarRecoveryInterval, avatarRetryDelay, loadWaAvatar, retryWaAvatar } from './wa-avatar-loader';

export function WaAvatarImage({ src, alt }: { src: string; alt: string }) {
  const query = useQuery<Blob | null>({
    queryKey: ['wa', 'picture', src],
    enabled: Boolean(src),
    staleTime: (state) => state.state.data ? 3600000 : 30000,
    gcTime: 3600000,
    retry: retryWaAvatar,
    retryDelay: avatarRetryDelay,
    refetchOnWindowFocus: true,
    refetchOnReconnect: true,
    refetchInterval: (query) => avatarRecoveryInterval(query.state),
    queryFn: ({ signal }) => loadWaAvatar(src, signal),
  });
  const [image, setImage] = useState<{ src: string; url: string }>();
  useEffect(() => {
    if (!query.data) return;
    const url = URL.createObjectURL(query.data);
    setImage({ src, url });
    return () => URL.revokeObjectURL(url);
  }, [query.data, src]);
  return image?.src === src ? <AvatarImage src={image.url} alt={alt} /> : null;
}
