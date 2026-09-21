export type User = {
  id: string;
  name: string;
  paymentIdentifier: string;
  role: string;
};

export type Account = {
  accountNumber: string;
  balancePaise: number;
  status: string;
};

export type Recipient = {
  name: string;
  paymentIdentifier: string;
  status: string;
};

export type Payment = {
  id: string;
  amountPaise: number;
  currency: string;
  note?: string;
  origin: string;
  state: string;
  createdAt: string;
  completedAt?: string;
  failureReason?: string;
  senderName: string;
  senderPaymentIdentifier: string;
  receiverName: string;
  receiverPaymentIdentifier: string;
  direction: "SENT" | "RECEIVED";
  sourceBankName?: string;
  sourceBankCode?: string;
  destinationBankName?: string;
  destinationBankCode?: string;
  durationMs?: number;
};

export type MerchantReceiveInfo = {
  paymentIdentifier: string;
  accountNumber: string;
  accountStatus: string;
};

export type View = "home" | "pay" | "transactions" | "details";
export type MerchantView = "m-home" | "m-receive" | "m-incoming" | "m-settlement" | "m-search";
