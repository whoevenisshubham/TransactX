# Customer account reads

`GET /api/accounts` lists the authenticated user's accounts. `GET
/api/accounts/{accountID}` returns an owned account; an unknown or foreign
account returns `404 ACCOUNT_NOT_FOUND`. Both require the existing bearer JWT.

The standard response envelope contains `requestId` and `data`. Each account
contains:

| Field | Meaning |
| --- | --- |
| `accountNumber` | Public account reference |
| `balancePaise` | Current central balance in integer paise |
| `status` | Stored account status |
| `bankCode` | Stored public bank catalogue code |
| `bankName` | Stored public bank catalogue name |

Bank metadata is an additive read-only projection joined by the account's bank
relation. It does not probe bank health, change routing or establish participant
settlement. The API excludes internal account, user, bank and participant IDs,
versions and opening balances. The list is ordered by creation time and account
ID; an empty owned list is `[]`.

Clients support responses from older deployments without bank metadata and show
an explicit unavailable label. They must not guess a bank from an account number
or a prior payment's route.
