import React, { useState } from "react";
import { Button } from "../components/ui/button";
import { Input } from "../components/ui/input";
import { Label } from "../components/ui/label";
import { Card, CardHeader, CardTitle, CardDescription, CardContent, CardFooter } from "../components/ui/card";
import { Badge, StatusBadge } from "../components/ui/badge";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "../components/ui/tabs";
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "../components/ui/table";
import { Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter, DialogClose } from "../components/ui/dialog";
import { Drawer, DrawerTrigger, DrawerContent, DrawerTitle, DrawerDescription } from "../components/ui/drawer";
import { Tooltip, TooltipTrigger, TooltipContent, TooltipProvider } from "../components/ui/tooltip";
import { ThemeToggle } from "../components/theme/ThemeToggle";
import { SimulationBadge } from "../components/feedback/SimulationBadge";
import { LoadingState, EmptyState, ErrorState } from "../components/feedback/StateFeedback";
import { ArrowRight, CheckCircle2, Copy, FileText, Layers, Send, ShieldAlert, Sparkles, Terminal } from "lucide-react";

export function ComponentShowcase() {
  const [copied, setCopied] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [dialogOpen, setDialogOpen] = useState(false);

  return (
    <TooltipProvider>
      <div className="space-y-12 pb-16 min-w-0">
        {/* Showcase Header */}
        <div className="flex flex-col md:flex-row md:items-center justify-between gap-4 p-4 sm:p-6 rounded-2xl border border-[var(--border-hairline)] bg-[var(--bg-surface)] shadow-sm min-w-0">
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2 mb-2">
              <span className="px-2 py-0.5 rounded text-[10px] font-mono font-semibold uppercase bg-[var(--status-info-bg)] text-[var(--status-info-fg)] border border-[var(--status-info-border)]">
                Design System v4.0
              </span>
              <SimulationBadge />
            </div>
            <h1 className="text-xl sm:text-2xl font-bold tracking-tight text-[var(--text-primary)]">
              TransactX Component Showcase & Design Architecture
            </h1>
            <p className="text-xs text-[var(--text-secondary)] mt-1">
              Authoritative visual specification for Direction A (Premium Modern Fintech) and Direction B (Refined Enterprise Dark).
            </p>
          </div>
          <div className="flex items-center gap-3 shrink-0">
            <ThemeToggle />
          </div>
        </div>

        {/* Section 1: Typography & Numbers */}
        <section className="space-y-4">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">1. Typographic Engine & Tabular Figures</h2>
            <p className="text-xs text-[var(--text-secondary)]">Inter sans-serif interface hierarchy paired strictly with JetBrains Mono tabular numeric figures.</p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <Card>
              <CardHeader>
                <CardTitle>Interface Typography (Inter)</CardTitle>
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
                <CardTitle>Telemetry & Integer Figures (JetBrains Mono)</CardTitle>
                <CardDescription>OpenType tabular figures (tnum) for financial parity and audit hashes</CardDescription>
              </CardHeader>
              <CardContent className="space-y-4 min-w-0">
                <div className="p-3 rounded-lg border border-[var(--border-hairline)] bg-[var(--bg-surface-raised)]/40 space-y-2">
                  <div className="flex justify-between items-center text-xs">
                    <span className="text-[var(--text-secondary)]">Total Debited Balance:</span>
                    <span className="font-mono font-semibold text-sm text-[var(--text-primary)]">₹1,48,250.00</span>
                  </div>
                  <div className="flex justify-between items-center text-xs">
                    <span className="text-[var(--text-secondary)]">Raw Integer Representation:</span>
                    <span className="font-mono text-xs text-[var(--text-secondary)]">14,825,000 paise</span>
                  </div>
                  <div className="flex justify-between items-center text-xs">
                    <span className="text-[var(--text-secondary)]">Invariant Parity Delta:</span>
                    <span className="font-mono text-xs text-[var(--status-success-fg)] font-semibold">Δ ₹0.00 (0 paise)</span>
                  </div>
                </div>

                <div className="min-w-0">
                  <span className="text-[10px] font-mono text-[var(--text-tertiary)]">SHA-256 CANONICAL DIGEST</span>
                  <div className="mt-1 flex items-center justify-between p-2 rounded bg-[var(--bg-surface-raised)] font-mono text-xs text-[var(--text-secondary)] border border-[var(--border-hairline)] min-w-0">
                    <span className="truncate min-w-0 pr-2">e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855</span>
                    <Tooltip>
                      <TooltipTrigger asChild>
                        <button
                          type="button"
                          aria-label="Copy SHA-256 hash"
                          onClick={() => {
                            navigator.clipboard.writeText("e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855");
                            setCopied(true);
                            setTimeout(() => setCopied(false), 2000);
                          }}
                          className="ml-2 p-1 text-[var(--text-secondary)] hover:text-[var(--text-primary)] cursor-pointer shrink-0"
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

        {/* Section 2: Buttons & Interactive Controls */}
        <section className="space-y-4">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">2. Interactive Controls & Actions</h2>
            <p className="text-xs text-[var(--text-secondary)]">Strictly styled interactive triggers adhering to 40px base height and calibrated focus rings.</p>
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

              <div>
                <span className="text-xs font-semibold text-[var(--text-secondary)] uppercase tracking-wider block mb-3">States</span>
                <div className="flex flex-wrap items-center gap-3">
                  <Button variant="primary" disabled>Disabled Primary</Button>
                  <Button variant="secondary" disabled>Disabled Secondary</Button>
                  <Button variant="primary" loading>Processing</Button>
                  <Button variant="accent" size="sm">Small (32px)</Button>
                  <Button variant="accent" size="md">Medium (40px)</Button>
                  <Button variant="accent" size="lg">Large (48px)</Button>
                </div>
              </div>
            </CardContent>
          </Card>
        </section>

        {/* Section 3: Form Controls */}
        <section className="space-y-4">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">3. Form Controls & Pre-Flight Fields</h2>
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

        {/* Section 4: Status Badges & Telemetry Chips */}
        <section className="space-y-4">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">4. Status Chips & Telemetry Badges</h2>
            <p className="text-xs text-[var(--text-secondary)]">Calibrated semantic badges without neon AI glows or unreadable micro-text.</p>
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

        {/* Section 5: High-Density Data Tables */}
        <section className="space-y-4 min-w-0">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">5. High-Density Tabular Ledger</h2>
            <p className="text-xs text-[var(--text-secondary)]">Strict 52px row height, tabular numeric alignment, and hover isolation.</p>
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

        {/* Section 6: Modals, Drawers & Tabs */}
        <section className="space-y-4 min-w-0">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">6. Overlays, Drawers & Segmented Tabs</h2>
            <p className="text-xs text-[var(--text-secondary)]">Radix-powered dialogs and drawers with modal backdrop blur and zero layout shifts.</p>
          </div>

          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 min-w-0">
            <Card className="min-w-0">
              <CardHeader>
                <CardTitle>Overlays & Modals</CardTitle>
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
              <CardContent>
                <Tabs defaultValue="tab-overview">
                  <TabsList className="w-full grid grid-cols-3">
                    <TabsTrigger value="tab-overview">Overview</TabsTrigger>
                    <TabsTrigger value="tab-routes">Routing</TabsTrigger>
                    <TabsTrigger value="tab-merkle">Merkle Tree</TabsTrigger>
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

        {/* Section 7: Feedback & System States */}
        <section className="space-y-4">
          <div className="border-b border-[var(--border-hairline)] pb-2">
            <h2 className="text-lg font-semibold text-[var(--text-primary)]">7. Loading, Empty & Error States</h2>
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
