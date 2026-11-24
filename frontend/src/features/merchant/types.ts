import type { Account, MerchantReceiveInfo, Payment, User } from "../../types";

export type MerchantView = "m-home" | "m-receive" | "m-incoming" | "m-settlement" | "m-search";

export type SettlementTone = "success" | "warning" | "error" | "neutral";

export interface MerchantViewProps {
  user: User;
  account?: Account;
  payments: Payment[];
  token?: string;
  onNavigate: (view: MerchantView) => void;
  onRefresh: () => void;
}

export type { Account, MerchantReceiveInfo, Payment, User };
