export type User = {
  id: string;
  name: string;
  paymentIdentifier: string;
  role: string;
};

export type Account = {
  id: string;
  accountNumber: string;
  balancePaise: number;
  status: string;
};

export type Recipient = {
  accountId: string;
  name: string;
  paymentIdentifier: string;
  accountStatus: string;
};

export type Payment = {
  id: string;
  amountPaise: number;
  currency: string;
  state: string;
  createdAt: string;
  completedAt?: string;
  failureReason?: string;
  counterpartyName: string;
  counterpartyPaymentIdentifier: string;
  senderAccountId: string;
  receiverAccountId: string;
};

export type View = "home" | "pay" | "transactions" | "details";
