import type { Account, Payment, Recipient, User, View } from "../../types";
import type { OfflineIntent } from "../../offlineQueue";
import type { DurableUnresolvedIntent, DurableUnresolvedStore } from "../../unresolvedStore";

export type CustomerView = View;

export interface CustomerShellProps {
  token: string;
  onLogout: () => void;
}

export interface CustomerDashboardProps {
  user: User | null;
  account?: Account;
  payments: Payment[];
  onNavigate: (view: CustomerView, paymentID?: string) => void;
  onExploreDemo: () => void;
}

export interface SendMoneyPageProps {
  account?: Account;
  token: string;
  onComplete: (payment: Payment) => void;
  onQueueChanged: () => void;
  onNavigate: (view: CustomerView) => void;
  queue: import("../../offlineQueue").OfflineIntentQueue;
  unresolvedStore?: DurableUnresolvedStore;
  unresolvedIntent?: DurableUnresolvedIntent | null;
  storeStatus?: "uninitialized" | "loading" | "ready" | "failed";
}

export interface TransactionActivityProps {
  payments: Payment[];
  onNavigate: (view: CustomerView, paymentID?: string) => void;
}

export interface ReceiptDrawerProps {
  paymentID: string;
  token: string;
  onNavigate: (view: CustomerView) => void;
  onClose?: () => void;
}

export interface OfflineQueuePanelProps {
  intents: OfflineIntent[];
  onRetry: (clientRequestId: string) => Promise<void>;
}
