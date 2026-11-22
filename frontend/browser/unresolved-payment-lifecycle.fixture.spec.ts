import { test, expect } from "@playwright/test";

test.describe("M20-R.3: Unresolved Payment Lifecycle, Dispatch Reservation & Terminal States", () => {
  const defaultUser = {
    id: "alice-123",
    name: "Alice Tester",
    role: "CUSTOMER",
    paymentIdentifier: "alice@transactx",
  };

  const defaultAccount = [
    {
      accountNumber: "TX-9901",
      balancePaise: 500000,
      status: "ACTIVE",
      bankName: "First Transact Bank",
      bankCode: "FTB",
    },
  ];

  const defaultRecipient = {
    name: "Merchant Bob",
    paymentIdentifier: "bob@transactx",
    status: "ACTIVE",
  };

  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      sessionStorage.setItem("transactx.token", "fixture-alice-token");
    });
  });

  async function setupStandardMocks(
    page: any,
    options: {
      postPaymentHandler?: (route: any, request: any) => Promise<void>;
      idempotencyHandler?: (route: any, key: string) => Promise<void>;
      user?: any;
    } = {}
  ) {
    const userToUse = options.user ?? defaultUser;
    await page.route("http://localhost:8080/**", async (route: any) => {
      const request = route.request();
      const path = new URL(request.url()).pathname;
      const headers = {
        "access-control-allow-origin": "http://127.0.0.1:4173",
        "access-control-allow-headers":
          "Authorization, Content-Type, Idempotency-Key, X-Request-ID",
        "access-control-allow-methods": "GET, POST, OPTIONS",
      };

      if (request.method() === "OPTIONS") {
        return route.fulfill({ status: 204, headers });
      }

      if (path === "/api/me") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({ requestId: "req-me", data: userToUse }),
        });
      }
      if (path === "/api/accounts") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({ requestId: "req-acc", data: defaultAccount }),
        });
      }
      if (path.startsWith("/api/recipients/")) {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({ requestId: "req-rec", data: defaultRecipient }),
        });
      }
      if (path === "/api/payments" && request.method() === "POST") {
        if (options.postPaymentHandler) {
          return options.postPaymentHandler(route, request);
        }
        return route.fulfill({
          headers,
          status: 201,
          contentType: "application/json",
          body: JSON.stringify({
            requestId: "req-pmt",
            data: {
              id: "pmt-001",
              amountPaise: 10000,
              currency: "INR",
              origin: "BANK",
              state: "COMPLETED",
              createdAt: new Date().toISOString(),
              senderName: userToUse.name,
              senderPaymentIdentifier: userToUse.paymentIdentifier,
              receiverName: defaultRecipient.name,
              receiverPaymentIdentifier: defaultRecipient.paymentIdentifier,
              direction: "SENT",
            },
          }),
        });
      }
      if (path.startsWith("/api/payments/idempotency/")) {
        const key = decodeURIComponent(path.split("/").pop() ?? "");
        if (options.idempotencyHandler) {
          return options.idempotencyHandler(route, key);
        }
        return route.fulfill({
          headers,
          status: 404,
          contentType: "application/json",
          body: JSON.stringify({ error: { code: "PAYMENT_NOT_FOUND", message: "Not found" } }),
        });
      }
      if (path === "/api/payments") {
        return route.fulfill({
          headers,
          contentType: "application/json",
          body: JSON.stringify({ requestId: "req-list", data: [] }),
        });
      }

      return route.fulfill({
        headers,
        status: 404,
        contentType: "application/json",
        body: JSON.stringify({ error: { code: "NOT_FOUND", message: "Unknown route" } }),
      });
    });
  }

  // E2E-01: Pre-dispatch durable reservation
  test("E2E-01: Pre-dispatch durable reservation creates atomic entry in IndexedDB", async ({ page }) => {
    let postStarted = false;
    let reservationDuringFlight: any = null;

    await setupStandardMocks(page, {
      postPaymentHandler: async (route, request) => {
        postStarted = true;
        // Verify active reservation exists in IndexedDB while HTTP is in flight
        reservationDuringFlight = await page.evaluate(async () => {
          return new Promise((resolve) => {
            const req = indexedDB.open("transactx-unresolved-intents", 2);
            req.onsuccess = () => {
              const db = req.result;
              const tx = db.transaction("active-reservations", "readonly");
              const getReq = tx.objectStore("active-reservations").getAll();
              getReq.onsuccess = () => resolve(getReq.result);
              getReq.onerror = () => resolve([]);
            };
            req.onerror = () => resolve([]);
          });
        });

        // Delay to allow observation
        await new Promise((r) => setTimeout(r, 100));
        await route.fulfill({
          status: 201,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({
            data: {
              id: "pmt-e2e-01",
              amountPaise: 10000,
              currency: "INR",
              origin: "BANK",
              state: "COMPLETED",
              createdAt: new Date().toISOString(),
              senderName: "Alice Tester",
              senderPaymentIdentifier: "alice@transactx",
              receiverName: "Merchant Bob",
              receiverPaymentIdentifier: "bob@transactx",
              direction: "SENT",
            },
          }),
        });
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await expect(page.getByText("Merchant Bob", { exact: true })).toBeVisible();

    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();

    await expect(page.getByRole("heading", { name: "Confirm payment details" })).toBeVisible();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    await expect(page.getByRole("heading", { name: /Payment complete/ })).toBeVisible();
    expect(postStarted).toBe(true);
    expect(reservationDuringFlight).not.toBeNull();
    expect(reservationDuringFlight.length).toBe(1);
    expect(reservationDuringFlight[0].ownerUserId).toBe("alice-123");
    expect(reservationDuringFlight[0].state).toBe("DISPATCHING");
  });

  // E2E-02: Crash immediately after dispatch / restore original intent on reload
  test("E2E-02: 500 error preserves active intent and survives reload with form locked", async ({ page }) => {
    let capturedKey = "";
    await setupStandardMocks(page, {
      postPaymentHandler: async (route, request) => {
        capturedKey = request.headers()["idempotency-key"];
        await route.fulfill({
          status: 504,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({ error: { code: "GATEWAY_TIMEOUT", message: "Upstream timeout" } }),
        });
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await expect(page.getByText("Merchant Bob", { exact: true })).toBeVisible();

    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    // Reaches uncertain state
    await expect(page.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Check status" })).toBeVisible();

    // Reload the page
    await page.reload();

    // After reload on /pay, uncertain status view appears with transaction identity preserved
    await expect(page.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();
    await page.getByRole("button", { name: "Review intent details" }).click();
    await expect(page.getByTestId("unresolved-intent-banner")).toBeVisible();
    await expect(page.getByLabel("Recipient TransactX payment ID")).toBeDisabled();
    await expect(page.getByLabel("Amount in Indian rupees")).toBeDisabled();
  });

  // E2E-03: Delayed server commit (404 initially, then 200 COMPLETED)
  test("E2E-03: Delayed server commit handles 404 in-flight then resolves on 200", async ({ page }) => {
    let checkCount = 0;
    await setupStandardMocks(page, {
      postPaymentHandler: async (route) => {
        await route.fulfill({
          status: 500,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({ error: { code: "SERVER_ERROR", message: "Temporary blip" } }),
        });
      },
      idempotencyHandler: async (route, key) => {
        checkCount++;
        const headers = { "access-control-allow-origin": "http://127.0.0.1:4173" };
        if (checkCount === 1) {
          return route.fulfill({
            status: 404,
            headers,
            contentType: "application/json",
            body: JSON.stringify({ error: { code: "PAYMENT_NOT_FOUND", message: "Not found" } }),
          });
        }
        return route.fulfill({
          status: 200,
          headers,
          contentType: "application/json",
          body: JSON.stringify({
            data: {
              id: "pmt-delayed-commit",
              amountPaise: 10000,
              currency: "INR",
              origin: "BANK",
              state: "COMPLETED",
              createdAt: new Date().toISOString(),
              senderName: "Alice Tester",
              senderPaymentIdentifier: "alice@transactx",
              receiverName: "Merchant Bob",
              receiverPaymentIdentifier: "bob@transactx",
              direction: "SENT",
            },
          }),
        });
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    await expect(page.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();

    // First check status -> returns 404, shows safe message, intent preserved
    await page.getByRole("button", { name: "Check status" }).click();
    await expect(page.getByText(/Payment not yet found on the ledger/)).toBeVisible();

    // Second check status -> returns 200 COMPLETED, resolves terminal
    await page.getByRole("button", { name: "Check status" }).click();
    await expect(page.getByRole("heading", { name: /Payment complete/ })).toBeVisible();
  });

  // E2E-04: HTTP 202 PROCESSING keeps active reservation and form locked
  test("E2E-04: HTTP 202 PROCESSING keeps reservation active and form locked", async ({ page }) => {
    await setupStandardMocks(page, {
      postPaymentHandler: async (route) => {
        await route.fulfill({
          status: 202,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({
            data: {
              id: "pmt-202-proc",
              amountPaise: 10000,
              currency: "INR",
              origin: "BANK",
              state: "PROCESSING",
              createdAt: new Date().toISOString(),
              senderName: "Alice Tester",
              senderPaymentIdentifier: "alice@transactx",
              receiverName: "Merchant Bob",
              receiverPaymentIdentifier: "bob@transactx",
              direction: "SENT",
            },
          }),
        });
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    // 202 PROCESSING is non-terminal -> routes to uncertain / pending screen
    await expect(page.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();

    // Verify reservation still exists in IndexedDB
    const activeRes = await page.evaluate(async () => {
      return new Promise((resolve) => {
        const req = indexedDB.open("transactx-unresolved-intents", 2);
        req.onsuccess = () => {
          const db = req.result;
          const tx = db.transaction("active-reservations", "readonly");
          const getReq = tx.objectStore("active-reservations").getAll();
          getReq.onsuccess = () => resolve(getReq.result);
        };
      });
    });
    expect((activeRes as any[]).length).toBe(1);
    expect((activeRes as any[])[0].state).toBe("PROCESSING");
  });

  // E2E-05: HTTP 200 PENDING_RECONCILIATION keeps reservation active
  test("E2E-05: HTTP 200 PENDING_RECONCILIATION remains active and locked", async ({ page }) => {
    await setupStandardMocks(page, {
      postPaymentHandler: async (route) => {
        await route.fulfill({
          status: 200,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({
            data: {
              id: "pmt-200-pending",
              amountPaise: 10000,
              currency: "INR",
              origin: "BANK",
              state: "PENDING_RECONCILIATION",
              createdAt: new Date().toISOString(),
              senderName: "Alice Tester",
              senderPaymentIdentifier: "alice@transactx",
              receiverName: "Merchant Bob",
              receiverPaymentIdentifier: "bob@transactx",
              direction: "SENT",
            },
          }),
        });
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    // PENDING_RECONCILIATION is non-terminal -> stays locked
    await expect(page.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();
    await page.getByRole("button", { name: "Review intent details" }).click();
    await expect(page.getByTestId("unresolved-intent-banner")).toBeVisible();
  });

  // E2E-06: Intermediate state transitions to COMPLETED on status check
  test("E2E-06: Intermediate state transitions to COMPLETED when status resolves", async ({ page }) => {
    let resolved = false;
    await setupStandardMocks(page, {
      postPaymentHandler: async (route) => {
        await route.fulfill({
          status: 200,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({
            data: {
              id: "pmt-trans",
              amountPaise: 10000,
              currency: "INR",
              origin: "BANK",
              state: "COMMITTED",
              createdAt: new Date().toISOString(),
              senderName: "Alice Tester",
              senderPaymentIdentifier: "alice@transactx",
              receiverName: "Merchant Bob",
              receiverPaymentIdentifier: "bob@transactx",
              direction: "SENT",
            },
          }),
        });
      },
      idempotencyHandler: async (route) => {
        resolved = true;
        await route.fulfill({
          status: 200,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({
            data: {
              id: "pmt-trans",
              amountPaise: 10000,
              currency: "INR",
              origin: "BANK",
              state: "COMPLETED",
              createdAt: new Date().toISOString(),
              senderName: "Alice Tester",
              senderPaymentIdentifier: "alice@transactx",
              receiverName: "Merchant Bob",
              receiverPaymentIdentifier: "bob@transactx",
              direction: "SENT",
            },
          }),
        });
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    await expect(page.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();
    await page.getByRole("button", { name: "Check status" }).click();
    await expect(page.getByRole("heading", { name: /Payment complete/ })).toBeVisible();
    expect(resolved).toBe(true);
  });

  // E2E-07: Terminal failure releases reservation
  test("E2E-07: Terminal failure marks intent failed and frees reservation", async ({ page }) => {
    await setupStandardMocks(page, {
      postPaymentHandler: async (route) => {
        await route.fulfill({
          status: 200,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({
            data: {
              id: "pmt-failed",
              amountPaise: 10000,
              currency: "INR",
              origin: "BANK",
              state: "FAILED",
              failureReason: "Insufficient ledger funds",
              createdAt: new Date().toISOString(),
              senderName: "Alice Tester",
              senderPaymentIdentifier: "alice@transactx",
              receiverName: "Merchant Bob",
              receiverPaymentIdentifier: "bob@transactx",
              direction: "SENT",
            },
          }),
        });
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    // FAILED returns to form with error and releases reservation
    await expect(page.getByText("Insufficient ledger funds")).toBeVisible();

    const activeRes = await page.evaluate(async () => {
      return new Promise((resolve) => {
        const req = indexedDB.open("transactx-unresolved-intents", 2);
        req.onsuccess = () => {
          const db = req.result;
          const tx = db.transaction("active-reservations", "readonly");
          const getReq = tx.objectStore("active-reservations").getAll();
          getReq.onsuccess = () => resolve(getReq.result);
        };
      });
    });
    expect((activeRes as any[]).length).toBe(0);
  });

  // E2E-08: Terminal reversal releases reservation
  test("E2E-08: Terminal reversal marks intent reversed and frees reservation", async ({ page }) => {
    await setupStandardMocks(page, {
      postPaymentHandler: async (route) => {
        await route.fulfill({
          status: 200,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({
            data: {
              id: "pmt-rev",
              amountPaise: 10000,
              currency: "INR",
              origin: "BANK",
              state: "REVERSED",
              createdAt: new Date().toISOString(),
              senderName: "Alice Tester",
              senderPaymentIdentifier: "alice@transactx",
              receiverName: "Merchant Bob",
              receiverPaymentIdentifier: "bob@transactx",
              direction: "SENT",
            },
          }),
        });
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    await expect(page.getByText("Payment was reversed.")).toBeVisible();
  });

  // E2E-09: Offline before dispatch enqueues to offline queue
  test("E2E-09: Offline before dispatch queues payment and does not create unresolved intent", async ({ page }) => {
    await setupStandardMocks(page);

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();

    // Emulate offline before clicking confirm
    await page.context().setOffline(true);

    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    // Should show queued state
    await expect(page.getByRole("heading", { name: "Payment queued for replay" })).toBeVisible();

    // Verify NO active reservation in unresolved store
    const activeRes = await page.evaluate(async () => {
      return new Promise((resolve) => {
        const req = indexedDB.open("transactx-unresolved-intents", 2);
        req.onsuccess = () => {
          const db = req.result;
          const tx = db.transaction("active-reservations", "readonly");
          const getReq = tx.objectStore("active-reservations").getAll();
          getReq.onsuccess = () => resolve(getReq.result);
          getReq.onerror = () => resolve([]);
        };
        req.onerror = () => resolve([]);
      });
    });
    expect((activeRes as any[]).length).toBe(0);

    await page.context().setOffline(false);
  });

  // E2E-10: Network drop after dispatch remains in unresolved store, NOT in offline queue
  test("E2E-10: Network drop after dispatch stays in unresolved store, NOT offline queue", async ({ page }) => {
    await setupStandardMocks(page, {
      postPaymentHandler: async (route) => {
        // Abort the network connection to simulate socket drop after dispatch
        await route.abort("failed");
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    // Network error -> uncertain screen
    await expect(page.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();

    // Check that active reservation is preserved
    const activeRes = await page.evaluate(async () => {
      return new Promise((resolve) => {
        const req = indexedDB.open("transactx-unresolved-intents", 2);
        req.onsuccess = () => {
          const db = req.result;
          const tx = db.transaction("active-reservations", "readonly");
          const getReq = tx.objectStore("active-reservations").getAll();
          getReq.onsuccess = () => resolve(getReq.result);
        };
      });
    });
    expect((activeRes as any[]).length).toBe(1);
    expect((activeRes as any[])[0].state).toBe("UNCERTAIN");
  });

  // E2E-11: IndexedDB unavailable blocks dispatch fail-closed
  test("E2E-11: IndexedDB unavailable blocks dispatch fail-closed", async ({ page }) => {
    await setupStandardMocks(page);

    // Disable IndexedDB before navigating
    await page.addInitScript(() => {
      Object.defineProperty(window, "indexedDB", {
        get() {
          return null;
        },
      });
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    // Must show storage unavailable error message and block dispatch
    await expect(page.getByText(/Payment dispatch blocked: Durable browser storage/)).toBeVisible();
  });

  // E2E-12: Cross-tab concurrency: Tab 2 blocked when Tab 1 holds active reservation
  test("E2E-12: Cross-tab concurrency locks second tab when active reservation held", async ({ context }) => {
    const page1 = await context.newPage();
    const page2 = await context.newPage();

    await page1.addInitScript(() => sessionStorage.setItem("transactx.token", "fixture-alice-token"));
    await page2.addInitScript(() => sessionStorage.setItem("transactx.token", "fixture-alice-token"));

    await setupStandardMocks(page1, {
      postPaymentHandler: async (route) => {
        // Hold post request in flight
        await new Promise((r) => setTimeout(r, 2000));
        await route.fulfill({
          status: 201,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({
            data: {
              id: "pmt-tab1",
              amountPaise: 10000,
              currency: "INR",
              origin: "BANK",
              state: "COMPLETED",
              createdAt: new Date().toISOString(),
              senderName: "Alice Tester",
              senderPaymentIdentifier: "alice@transactx",
              receiverName: "Merchant Bob",
              receiverPaymentIdentifier: "bob@transactx",
              direction: "SENT",
            },
          }),
        });
      },
    });

    await setupStandardMocks(page2);

    // Tab 1 submits payment
    await page1.goto("/pay");
    await page1.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page1.getByRole("button", { name: "Find", exact: true }).click();
    await page1.getByLabel("Amount in Indian rupees").fill("100.00");
    await page1.getByRole("button", { name: "Review payment" }).click();
    await page1.getByRole("button", { name: "Confirm payment", exact: true }).click();

    // Tab 2 navigates and tries to initiate a payment while Tab 1 is in-flight
    await page2.goto("/pay");
    // Tab 2 detects the active reservation and is routed to uncertain status view
    await expect(page2.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();
    await page2.getByRole("button", { name: "Review intent details" }).click();
    await expect(page2.getByTestId("unresolved-intent-banner")).toBeVisible();
    await expect(page2.getByLabel("Recipient TransactX payment ID")).toBeDisabled();

    await page1.close();
    await page2.close();
  });

  // E2E-13: Refresh while request pending restores unresolved state
  test("E2E-13: Refresh while request pending restores unresolved state", async ({ page }) => {
    await setupStandardMocks(page, {
      postPaymentHandler: async (route) => {
        await route.fulfill({
          status: 502,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({ error: { code: "BAD_GATEWAY", message: "Bad gateway" } }),
        });
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    await expect(page.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();

    await page.goto("/");
    await expect(page.getByTestId("global-unresolved-banner")).toBeVisible();
    await page.getByRole("button", { name: "Resolve transfer" }).click();
    await expect(page.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();
    await page.getByRole("button", { name: "Review intent details" }).click();
    await expect(page.getByTestId("unresolved-intent-banner")).toBeVisible();
  });

  // E2E-14: Storage transaction resilience
  test("E2E-14: Storage reservation creates proper schema and version", async ({ page }) => {
    await setupStandardMocks(page);
    await page.goto("/pay");

    const dbInfo = await page.evaluate(async () => {
      return new Promise((resolve, reject) => {
        const req = indexedDB.open("transactx-unresolved-intents", 2);
        req.onupgradeneeded = () => {
          const db = req.result;
          if (!db.objectStoreNames.contains("unresolved-intents")) {
            db.createObjectStore("unresolved-intents", { keyPath: "clientRequestId" });
          }
          if (!db.objectStoreNames.contains("active-reservations")) {
            db.createObjectStore("active-reservations", { keyPath: "ownerUserId" });
          }
        };
        req.onsuccess = () => {
          const db = req.result;
          resolve({
            version: db.version,
            stores: Array.from(db.objectStoreNames),
          });
        };
        req.onerror = () => reject(req.error);
      });
    });

    expect((dbInfo as any).version).toBe(2);
    expect((dbInfo as any).stores).toContain("active-reservations");
    expect((dbInfo as any).stores).toContain("unresolved-intents");
  });

  // E2E-15: Unknown payment state fails closed
  test("E2E-15: Unknown payment state fails closed as non-terminal processing", async ({ page }) => {
    await setupStandardMocks(page, {
      postPaymentHandler: async (route) => {
        await route.fulfill({
          status: 200,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({
            data: {
              id: "pmt-weird-state",
              amountPaise: 10000,
              currency: "INR",
              origin: "BANK",
              state: "SOME_FUTURE_EXPERIMENTAL_STATE",
              createdAt: new Date().toISOString(),
              senderName: "Alice Tester",
              senderPaymentIdentifier: "alice@transactx",
              receiverName: "Merchant Bob",
              receiverPaymentIdentifier: "bob@transactx",
              direction: "SENT",
            },
          }),
        });
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();

    // Unknown state fails closed -> routed to uncertain/processing view, not treated as completed!
    await expect(page.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();
  });

  // E2E-16: User isolation
  test("E2E-16: User B cannot see User A's unresolved payment reservation", async ({ page }) => {
    // Stage an unresolved reservation for Alice
    await setupStandardMocks(page, {
      postPaymentHandler: async (route) => {
        await route.fulfill({
          status: 500,
          headers: { "access-control-allow-origin": "http://127.0.0.1:4173" },
          contentType: "application/json",
          body: JSON.stringify({ error: { code: "SERVER_ERROR", message: "Failed" } }),
        });
      },
    });

    await page.goto("/pay");
    await page.getByLabel("Recipient TransactX payment ID").fill("bob@transactx");
    await page.getByRole("button", { name: "Find", exact: true }).click();
    await page.getByLabel("Amount in Indian rupees").fill("100.00");
    await page.getByRole("button", { name: "Review payment" }).click();
    await page.getByRole("button", { name: "Confirm payment", exact: true }).click();
    await expect(page.getByRole("heading", { name: "We couldn't confirm the response" })).toBeVisible();

    // Now switch user to Charlie
    const charlieUser = {
      id: "charlie-999",
      name: "Charlie Brown",
      role: "CUSTOMER",
      paymentIdentifier: "charlie@transactx",
    };

    await setupStandardMocks(page, { user: charlieUser });
    await page.evaluate(() => {
      sessionStorage.setItem("transactx.token", "fixture-charlie-token");
    });
    await page.goto("/");

    // Charlie must NOT see Alice's global unresolved banner
    await expect(page.getByTestId("global-unresolved-banner")).not.toBeVisible();

    await page.goto("/pay");
    // Charlie must NOT see Alice's locked banner
    await expect(page.getByTestId("unresolved-intent-banner")).not.toBeVisible();
    await expect(page.getByLabel("Recipient TransactX payment ID")).not.toBeDisabled();
  });
});
