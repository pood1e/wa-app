import { useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Loader2, LogIn, LogOut, Trash2 } from "lucide-react";
import { useNavigate } from "react-router";
import { LongConnectionStatus } from "../proto/byte/v/forge/waapp/v1/messaging";
import { useWaLongConnectionIndex } from "./wa-long-connection-badge";
import { waReloginPath } from "./wa-route-paths";
import {
  WAAccountStatus,
  type WAAccount,
} from "../proto/byte/v/forge/waapp/v1/profile";
import { logoutWaAccount } from "./wa-api";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

type Props = {
  account: WAAccount;
  busy: boolean;
  onDelete: (account: WAAccount) => void;
  onDone: (message: string) => void;
  onError: (message: string) => void;
  onAccountChanged: () => void;
};

export function WaAccountActions({
  account,
  busy,
  onDelete,
  onDone,
  onError,
  onAccountChanged,
}: Props) {
  const navigate = useNavigate();
  const [action, setAction] = useState<"logout" | "remove" | null>(null);
  const logout = useMutation({
    mutationFn: logoutWaAccount,
    onSuccess() {
      setAction(null);
      onDone("已退出登录，账号资料和历史消息已保留");
    },
    onError(error) {
      onError(error.message);
    },
    onSettled: onAccountChanged,
  });
  const pending = busy || logout.isPending;
  const { byAccount } = useWaLongConnectionIndex();
  const connection = byAccount.get(account.wa_account_id);
  const relogin = account.status === WAAccountStatus.WA_ACCOUNT_STATUS_LOGGED_OUT || connection?.status === LongConnectionStatus.LONG_CONNECTION_STATUS_STOPPED;

  function choose(next: typeof action) {
    if (!pending) setAction(next);
  }

  return (
    <>
      <div className="flex shrink-0 items-center gap-2">
        {relogin ? <Button disabled={pending} size="sm" variant="outline" onClick={() => navigate(waReloginPath(account.wa_account_id))}><LogIn size={16} />重新登录</Button> : <Button
          variant="outline"
          size="sm"
          disabled={
            pending ||
            account.status !== WAAccountStatus.WA_ACCOUNT_STATUS_ACTIVE
          }
          onClick={() => choose("logout")}
        >
          <LogOut size={16} />
          退出登录
        </Button>}
        <Button
          variant="destructive"
          size="icon"
          disabled={pending}
          title="移除本地账号"
          aria-label="移除本地账号"
          onClick={() => choose("remove")}
        >
          <Trash2 size={16} />
        </Button>
      </div>
      <Dialog
        open={action !== null}
        onOpenChange={(open) => {
          if (!open) choose(null);
        }}
      >
        <DialogContent
          showCloseButton={!pending}
          onEscapeKeyDown={(event) => {
            if (pending) event.preventDefault();
          }}
          onInteractOutside={(event) => {
            if (pending) event.preventDefault();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              {action === "logout" ? "退出当前登录" : "移除本地账号"}
            </DialogTitle>
            <DialogDescription>
              {action === "logout"
                ? "停止消息收发，保留账号资料、联系人和历史消息。可随时重新登录。"
                : "删除本地保存的账号、登录材料和消息。WhatsApp 账号仍然保留。"}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={pending}
              onClick={() => choose(null)}
            >
              取消
            </Button>
            <Button
              variant={action === "logout" ? "default" : "destructive"}
              disabled={pending}
              onClick={() => {
                if (action === "logout") logout.mutate(account);
                else onDelete(account);
              }}
            >
              {pending ? <Loader2 className="animate-spin" size={16} /> : null}
              {action === "logout" ? "确认退出" : "移除本地账号"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
