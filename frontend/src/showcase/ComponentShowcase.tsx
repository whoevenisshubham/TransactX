import React, { useState } from "react";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "../components/ui/card";
import { Badge, StatusBadge } from "../components/ui/badge";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "../components/ui/tabs";
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "../components/ui/table";
import { Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from "../components/ui/dialog";
import { Drawer, DrawerTrigger, DrawerContent, DrawerTitle, DrawerDescription } from "../components/ui/drawer";
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "../components/ui/tooltip";
import { ThemeToggle } from "../components/theme/ThemeToggle";
import { SimulationBadge } from "../components/feedback/SimulationBadge";
import { LoadingState, EmptyState, ErrorState } from "../components/feedback/StateFeedback";
import { Avatar, Amount } from "../components";
import { ArrowRight, CheckCircle2, Copy, FileText, Send, ShieldAlert } from "lucide-react";

const TOKEN_SWATCHES = [
  { name: "Canvas", token: "--bg-canvas", hex: "#F8FAFC / #0B0F19" },
  { name: "Surface", token: "--bg-surface", hex: "#FFFFFF / #131B2E" },
  { name: "Raised", token: "--bg-surface-raised", hex: "#F1F5F9 / #1E293B" },
  { name: "Primary Ink", token: "--text-primary", hex: "#0F172A / #F8FAFC" },
  { name: "Primary Action", token: "--action-primary-bg", hex: "#0F172A / #F8FAFC" },
  { name: "Cobalt Accent", token: "--action-accent-bg", hex: "#1D4ED8 / #2563EB" },
  { name: "Settled", token: "--status-success-fg", hex: "#166534 / #6EE7B7" },
  { name: "Recovering", token: "--status-warning-fg", hex: "#92400E / #FDE047" },
];

const REPAIRED_CSS_TOKENS = [
  "--foreground",
  "--bg-subtle",
  "--border-color",
  "--reset-gold",
  "--warning-strong",
  "--warning-pale",
  "--warning-line",
];

export function ComponentShowcase() {
  const [copied, setCopied] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);

  return (
    <TooltipProvider>
      <div className="space-y-12 pb-16 min-w-0">
        {/* Showcase Header */}
        <div className="flex flex-col lg:flex-row lg:items-center justify-between gap-4 p-4 sm:p-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-[var(--shadow-subtle)] min-w-0">
          <div className="min-w-0 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant="info">Design System v4.1 · Slice 0</Badge>
              <SimulationBadge />
            </div>
            <h1 className="text-xl sm:text-2xl font-bold tracking-tight text-[var(--text-primary)]">
              TransactX Component Showcase &amp; Design Architecture
            </h1>
            <p className="text-xs sm:text-sm text-[var(--text-secondary)] max-w-2xl">
              Unified specification combining TransactX Financial (Light) &amp; TransactX Precision Dark palettes with responsive multi-role shell framing.
            </p>
          </div>
          <div className="flex items-center gap-3 shrink-0 self-start lg:self-center">
            <ThemeToggle />
          </div>
        </div>

        {/* Section 0: Semantic Color Architecture & Repaired CSS Variables */}
        <section className="space-y-4">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">1. Semantic Token Matrix &amp; Dual-Theme Parity</h2>
            <p className="text-xs text-[var(--text-secondary)]">Single source of truth in tokens.css — matte WCAG AA contrast, zero pure black (#000000), zero neon glows.</p>
          </div>

          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            {TOKEN_SWATCHES.map((item) => (
              <div
                key={item.token}
                className="p-3 rounded-xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] flex flex-col gap-2 min-w-0"
              >
                <div
                  className="h-9 w-full rounded-lg border border-[var(--border-hairline)]"
                  style={{ backgroundColor: `var(${item.token})` }}
                  aria-hidden="true"
                />
                <div className="min-w-0">
                  <div className="text-xs font-semibold text-[var(--text-primary)] truncate">{item.name}</div>
                  <div className="text-[11px] font-mono text-[var(--text-tertiary)] truncate">{item.token}</div>
                  <div className="text-[10px] font-mono text-[var(--text-secondary)] truncate">{item.hex}</div>
                </div>
              </div>
            ))}
          </div>

          <Card>
            <CardContent className="pt-5 pb-5 flex flex-wrap items-center justify-between gap-3">
              <div className="space-y-0.5 min-w-0">
                <p className="text-xs font-semibold text-[var(--text-primary)]">Legacy Variable Parity Verified</p>
                <p className="text-xs text-[var(--text-secondary)]">
                  All 7 previously undefined CSS custom properties resolve consistently in both Light and Dark modes.
                </p>
              </div>
              <div className="flex flex-wrap gap-1.5">
                {REPAIRED_CSS_TOKENS.map((token) => (
                  <code
                    key={token}
                    className="px-2 py-0.5 rounded-md text-[11px] font-mono border border-[var(--border-color)] bg-[var(--bg-subtle)] text-[var(--foreground)]"
                  >
                    {token}
                  </code>
                ))}
              </div>
            </CardContent>
          </Card>
        </section>

        {/* Section 2: Typography & Numbers */}
        <section className="space-y-4">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">2. Typographic Engine &amp; Tabular Figures</h2>
            <p className="text-xs text-[var(--text-secondary)]">Inter Variable (cv05) paired strictly with JetBrains Mono tabular numeric figures (tnum).</p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <Card>
              <CardHeader>
                <CardTitle>Interface Typography (Inter Variable)</CardTitle>
                <CardDescription>Calibrated display, heading, and body scales with negative tracking</CardDescription>
              </CardHeader>
              <CardContent className="space-y-3">
                <div>
                  <span className="text-[10px] font-mono text-[var(--text-tertiary)]">DISPLAY XL (40px / 600)</span>
                  <p className="text-3xl sm:text-4xl font-semibold tracking-tight text-[var(--text-primary)] leading-tight">
                    Instant Multi-Bank Settlement
                  </p>
                </div>
                <div>
                  <span className="text-[10px] font-mono text-[var(--text-tertiary)]">HEADING LG (24px / 600)</span>
                  <p className="text-xl sm:text-2xl font-semibold tracking-tight text-[var(--text-primary)]">
                    Two-Phase Commit Orchestrator
                  </p>
                </div>
                <div>
                  <span className="text-[10px] font-mono text-[var(--text-tertiary)]">BODY MD (14px / 400)</span>
                  <p className="text-sm text-[var(--text-secondary)] leading-relaxed">
                    Zero phantom debits across independent PostgreSQL participant schemas with monotonic lease recovery.
                  </p>
                </div>
              </CardContent>
            </Card>

            <Card className="min-w-0">
              <CardHeader>
                <CardTitle>Telemetry &amp; Integer Figures (JetBrains Mono)</CardTitle>
                <CardDescription>OpenType tabular figures (tnum) for integer paise parity and audit hashes</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 min-w-0">
                <div className="p-3 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/50 space-y-2">
                  <div className="flex justify-between items-center text-xs gap-2">
                    <span className="text-[var(--text-secondary)]">Total Debited Balance:</span>
                    <span className="font-mono font-semibold text-sm text-[var(--text-primary)]">
                      <Amount paise={14825000} />
                    </span>
                  </div>
                  <div className="flex justify-between items-center text-xs gap-2">
                    <span className="text-[var(--text-secondary)]">Raw Integer Representation:</span>
                    <span className="font-mono text-xs text-[var(--text-secondary)]">14,825,000 paise</span>
                  </div>
                  <div className="flex justify-between items-center text-xs gap-2">
                    <span className="text-[var(--text-secondary)]">Invariant Parity Delta:</span>
                    <span className="font-mono text-xs text-[var(--status-success-fg)] font-semibold">Δ ₹0.00 (0 paise)</span>
                  </div>
                </div>

                <div className="min-w-0">
                  <span className="text-[10px] font-mono text-[var(--text-tertiary)]">SHA-256 CANONICAL DIGEST</span>
                  <div className="mt-1 flex items-center justify-between p-2 rounded-lg bg-[var(--bg-surface-raised)] font-mono text-xs text-[var(--text-secondary)] border border-[var(--border-hairline)] min-w-0">
                    <span className="truncate min-w-0 pr-2">e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855</span>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          aria-label="Copy SHA-256 hash"
                          onClick={() => {
                            navigator.clipboard?.writeText("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
                            setCopied(true);
                            setTimeout(() => setCopied(false), 2000);
                          }}
                          className="ml-2 p-1 rounded text-[var(--text-secondary)] hover:text-[var(--text-primary)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--border-focus)] cursor-pointer shrink-0"
                        >
                          {copied ? <CheckCircle2 className="w-3.5 h-3.5 text-[var(--status-success-fg)]" /> : <Copy className="w-3.5 h-3.5" />}
                        </button>
                      </TooltipTrigger>
                      <TooltipContent>{copied ? "Copied!" : "Copy hash"}</TooltipContent>
                    </Tooltip>
                  </div>
                </div>
              </CardContent>
            </Card>
          </div>
        </section>

        {/* Section 3: Buttons, Avatars & Interactive Controls */}
        <section className="space-y-4">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">3. Interactive Controls, Buttons &amp; Circular Avatars</h2>
            <p className="text-xs text-[var(--text-secondary)]">Calibrated focus rings, 40px base height, and distortion-free square/circular avatars.</p>
          </div>

          <Card>
            <CardContent className="space-y-6 pt-6">
              <div>
                <span className="text-xs font-semibold text-[var(--text-secondary)] uppercase tracking-wider block mb-3">Variants</span>
                <div className="flex flex-wrap items-center gap-3">
                  <Button variant="primary">Primary Action</Button>
                  <Button variant="secondary">Secondary Action</Button>
                  <Button variant="accent">
                    <Send className="w-3.5 h-3.5" />
                    <span>Send Payment</span>
                  </Button>
                  <Button variant="outline">Outline Trigger</Button>
                  <Button variant="ghost">Ghost Trigger</Button>
                  <Button variant="destructive">
                    <ShieldAlert className="w-3.5 h-3.5" />
                    <span>Halt Circuit</span>
                  </Button>
                </div>
              </div>

              <div className="grid grid-cols-1 md:grid-cols-2 gap-6 pt-2 border-t border-[var(--border-hairline)]">
                <div>
                  <span className="text-xs font-semibold text-[var(--text-secondary)] uppercase tracking-wider block mb-3">States &amp; Sizing</span>
                  <div className="flex flex-wrap items-center gap-3">
                    <Button variant="primary" disabled>Disabled Primary</Button>
                    <Button variant="secondary" disabled>Disabled Secondary</Button>
                    <Button variant="primary" loading>Processing</Button>
                    <Button variant="accent" size="sm">Small (32px)</Button>
                    <Button variant="accent" size="md">Medium (40px)</Button>
                    <Button variant="accent" size="lg">Large (48px)</Button>
                  </div>
                </div>

                <div className="min-w-0">
                  <span className="text-xs font-semibold text-[var(--text-secondary)] uppercase tracking-wider block mb-3">Entity Avatars (Aspect-Locked)</span>
                  <div className="flex flex-wrap items-center gap-3 p-3 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/40 min-w-0">
                    <Avatar name="Alice Demo" size="sm" />
                    <Avatar name="Demo Store" size="md" />
                    <Avatar name="Bank B Pacific" size="lg" />
                    <span className="text-xs text-[var(--text-secondary)] min-w-0 flex-1 basis-40">
                      Zero horizontal stretch inside flex containers (shrink-0 aspect-square).
                    </span>
                  </div>
                </div>
              </div>
            </CardContent>
          </Card>
        </section>

        {/* Section 4: Form Controls */}
        <section className="space-y-4">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">4. Form Controls &amp; Pre-Flight Fields</h2>
            <p className="text-xs text-[var(--text-secondary)]">Inputs with explicit focus transitions, error rings, and currency adornments.</p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <Card>
              <CardContent className="pt-6 space-y-4">
                <div>
                  <Label htmlFor="recipient-input" required>Recipient Payment Identifier</Label>
                  <Input id="recipient-input" placeholder="e.g. store@transactx" defaultValue="store@transactx" />
                  <p className="text-[11px] text-[var(--text-tertiary)] mt-1">TransactX participant ID only.</p>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardContent className="pt-6 space-y-4">
                <div>
                  <Label htmlFor="amount-input" required>Transfer Amount</Label>
                  <Input
                    id="amount-input"
                    startAdornment={<span className="font-semibold text-xs text-[var(--text-primary)]">₹</span>}
                    endAdornment={<span className="font-mono text-xs text-[var(--text-tertiary)]">INR</span>}
                    placeholder="0.00"
                    defaultValue="250.00"
                  />
                  <p className="text-[11px] text-[var(--text-tertiary)] mt-1 font-mono">Parsed: 25,000 paise</p>
                </div>
              </CardContent>
            </Card>

            <Card>
              <CardContent className="pt-6 space-y-4">
                <div>
                  <Label htmlFor="error-input">Simulated Validation Error</Label>
                  <Input id="error-input" error defaultValue="invalid_payment_id" aria-invalid="true" />
                  <p className="text-[11px] text-[var(--status-danger-fg)] mt-1 font-medium">Participant account not found.</p>
                </div>
              </CardContent>
            </Card>
          </div>
        </section>

        {/* Section 5: Status Badges & Telemetry Chips */}
        <section className="space-y-4">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">5. Status Chips &amp; Telemetry Badges</h2>
            <p className="text-xs text-[var(--text-secondary)]">Matte WCAG AA semantic badges without neon glows or illegible micro-text.</p>
          </div>

          <Card>
            <CardContent className="pt-6">
              <div className="flex flex-wrap items-center gap-3">
                <StatusBadge status="COMMITTED" />
                <StatusBadge status="COMPLETED" />
                <StatusBadge status="PROCESSING" />
                <StatusBadge status="RECOVERING" />
                <StatusBadge status="PENDING_RECONCILIATION" />
                <StatusBadge status="FAILED" />
                <StatusBadge status="CLOSED" />
                <StatusBadge status="HALF_OPEN" />
                <StatusBadge status="OPEN" />
                <Badge variant="neutral" dot>QUEUED (OFFLINE)</Badge>
              </div>
            </CardContent>
          </Card>
        </section>

        {/* Section 6: High-Density Data Tables */}
        <section className="space-y-4 min-w-0">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">6. High-Density Tabular Ledger</h2>
            <p className="text-xs text-[var(--text-secondary)]">Strict 52px row height, tabular numeric alignment, and responsive horizontal scroll containment.</p>
          </div>

          <div className="w-full min-w-0 overflow-hidden">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="whitespace-nowrap">Transaction ID</TableHead>
                  <TableHead className="whitespace-nowrap">Corridor</TableHead>
                  <TableHead className="whitespace-nowrap">Amount</TableHead>
                  <TableHead className="whitespace-nowrap">Saga State</TableHead>
                  <TableHead className="whitespace-nowrap">Latency</TableHead>
                  <TableHead className="whitespace-nowrap text-right">Action</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                <TableRow>
                  <TableCell className="font-mono text-xs font-semibold text-[var(--action-accent-bg)] whitespace-nowrap">tx_8902a_apex</TableCell>
                  <TableCell className="whitespace-nowrap">Bank A (Apex) → Bank B (Pacific)</TableCell>
                  <TableCell className="font-mono font-medium whitespace-nowrap">₹150.00 <span className="text-[11px] text-[var(--text-tertiary)]">(15,000 paise)</span></TableCell>
                  <TableCell className="whitespace-nowrap"><StatusBadge status="COMMITTED" /></TableCell>
                  <TableCell className="font-mono text-xs text-[var(--text-secondary)] whitespace-nowrap">78ms</TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    <Button variant="ghost" size="sm" onClick={() => setDrawerOpen(true)}>
                      <span>Receipt</span>
                      <ArrowRight className="w-3 h-3" />
                    </Button>
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableCell className="font-mono text-xs font-semibold text-[var(--action-accent-bg)] whitespace-nowrap">tx_8902b_pacific</TableCell>
                  <TableCell className="whitespace-nowrap">Bank B (Pacific) → Bank A (Apex)</TableCell>
                  <TableCell className="font-mono font-medium whitespace-nowrap">₹2,450.00 <span className="text-[11px] text-[var(--text-tertiary)]">(245,000 paise)</span></TableCell>
                  <TableCell className="whitespace-nowrap"><StatusBadge status="RECOVERING" /></TableCell>
                  <TableCell className="font-mono text-xs text-[var(--text-secondary)] whitespace-nowrap">142ms</TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    <Button variant="ghost" size="sm" onClick={() => setDrawerOpen(true)}>
                      <span>Evidence</span>
                      <ArrowRight className="w-3 h-3" />
                    </Button>
                  </TableCell>
                </TableRow>
                <TableRow>
                  <TableCell className="font-mono text-xs font-semibold text-[var(--action-accent-bg)] whitespace-nowrap">tx_8902c_apex</TableCell>
                  <TableCell className="whitespace-nowrap">Bank A (Apex) → Bank B (Pacific)</TableCell>
                  <TableCell className="font-mono font-medium whitespace-nowrap">₹50,000.00 <span className="text-[11px] text-[var(--text-tertiary)]">(5,000,000 paise)</span></TableCell>
                  <TableCell className="whitespace-nowrap"><StatusBadge status="FAILED" /></TableCell>
                  <TableCell className="font-mono text-xs text-[var(--text-secondary)] whitespace-nowrap">502ms</TableCell>
                  <TableCell className="text-right whitespace-nowrap">
                    <Button variant="ghost" size="sm" onClick={() => setDrawerOpen(true)}>
                      <span>Trace</span>
                      <ArrowRight className="w-3 h-3" />
                    </Button>
                  </TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </div>
        </section>

        {/* Section 7: Modals, Drawers & Tabs */}
        <section className="space-y-4 min-w-0">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">7. Overlays, Drawers &amp; Segmented Tabs</h2>
            <p className="text-xs text-[var(--text-secondary)]">Radix-powered dialogs and drawers with modal backdrop blur and zero layout shifts.</p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 min-w-0">
            <Card className="min-w-0">
              <CardHeader>
                <CardTitle>Overlays &amp; Modals</CardTitle>
                <CardDescription>Accessible Dialog and Side-Drawer primitives</CardDescription>
              </CardHeader>
              <CardContent className="flex flex-wrap items-center gap-3">
                <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
                  <DialogTrigger asChild>
                    <Button variant="secondary">Launch Confirmation Modal</Button>
                  </DialogTrigger>
                  <DialogContent>
                    <DialogHeader>
                      <DialogTitle>Confirm Outbox Sweep</DialogTitle>
                      <DialogDescription>
                        This operation acquires an advisory lock and expedites recovery for all pending payment sagas.
                      </DialogDescription>
                    </DialogHeader>
                    <div className="py-2 text-xs text-[var(--text-secondary)]">
                      Worker node lease epoch: <code className="font-mono font-semibold text-[var(--text-primary)]">#epoch_14028</code>
                    </div>
                    <DialogFooter>
                      <Button variant="outline" onClick={() => setDialogOpen(false)}>Cancel</Button>
                      <Button variant="primary" onClick={() => setDialogOpen(false)}>Execute Sweep</Button>
                    </DialogFooter>
                  </DialogContent>
                </Dialog>

                <Drawer open={drawerOpen} onOpenChange={setDrawerOpen}>
                  <DrawerTrigger asChild>
                    <Button variant="accent">Open Receipt Drawer</Button>
                  </DrawerTrigger>
                  <DrawerContent>
                    <div className="space-y-4">
                      <div>
                        <span className="text-[10px] font-mono text-[var(--text-tertiary)] uppercase">Proof of Payment</span>
                        <DrawerTitle className="text-2xl font-bold text-[var(--text-primary)] mt-1">₹150.00</DrawerTitle>
                        <DrawerDescription className="text-xs text-[var(--status-success-fg)] font-semibold mt-0.5">
                          ● SETTLED IN 78ms
                        </DrawerDescription>
                      </div>

                      <div className="space-y-2 p-3 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/50 text-xs">
                        <div className="flex justify-between">
                          <span className="text-[var(--text-secondary)]">Transaction ID:</span>
                          <span className="font-mono font-semibold">tx_8902a_apex</span>
                        </div>
                        <div className="flex justify-between">
                          <span className="text-[var(--text-secondary)]">Sender Account:</span>
                          <span className="font-mono">TX-APEX-001</span>
                        </div>
                        <div className="flex justify-between">
                          <span className="text-[var(--text-secondary)]">Receiver Account:</span>
                          <span className="font-mono">TX-PACIFIC-002</span>
                        </div>
                        <div className="flex justify-between">
                          <span className="text-[var(--text-secondary)]">Idempotency Key:</span>
                          <span className="font-mono">c8f6a449a087</span>
                        </div>
                      </div>

                      <div className="pt-2">
                        <Button variant="primary" className="w-full">
                          <FileText className="w-3.5 h-3.5" />
                          <span>Export Evidence Bundle (JSON)</span>
                        </Button>
                      </div>
                    </div>
                  </DrawerContent>
                </Drawer>
              </CardContent>
            </Card>

            <Card className="min-w-0">
              <CardHeader>
                <CardTitle>Segmented Tabs Control</CardTitle>
                <CardDescription>Radix Tabs with pill-track geometry</CardDescription>
              </CardHeader>
              <CardContent className="min-w-0">
                <Tabs defaultValue="tab-overview">
                  <TabsList className="w-full justify-between">
                    <TabsTrigger value="tab-overview" className="flex-1">Overview</TabsTrigger>
                    <TabsTrigger value="tab-routes" className="flex-1">Routing</TabsTrigger>
                    <TabsTrigger value="tab-merkle" className="flex-1">Merkle Tree</TabsTrigger>
                  </TabsList>
                  <TabsContent value="tab-overview" className="p-3 text-xs text-[var(--text-secondary)]">
                    Active cluster consensus term is healthy. 2 of 2 bank circuits flowing nominally.
                  </TabsContent>
                  <TabsContent value="tab-routes" className="p-3 text-xs text-[var(--text-secondary)]">
                    Adaptive routing dynamically distributes 50/50 between Apex National and Pacific Reserve.
                  </TabsContent>
                  <TabsContent value="tab-merkle" className="p-3 text-xs text-[var(--text-secondary)]">
                    Merkle tree root #8f902c verified. Cryptographic parity confirmed.
                  </TabsContent>
                </Tabs>
              </CardContent>
            </Card>
          </div>
        </section>

        {/* Section 8: Feedback & System States */}
        <section className="space-y-4">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">8. Loading, Empty &amp; Error States</h2>
            <p className="text-xs text-[var(--text-secondary)]">Standardized feedback patterns respecting reduced-motion preferences.</p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <LoadingState />
            <EmptyState actionLabel="Refresh View" onAction={() => {}} />
            <ErrorState onRetry={() => {}} />
          </div>
        </section>
      </div>
    </TooltipProvider>
  );
}
