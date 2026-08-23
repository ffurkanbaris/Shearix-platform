"use client";

import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { ApiError, apiClient } from "@/lib/api";
import { eligibleBarbers, idempotencyForSelection, isBookingConfig, selectionAfterChange, validGuest } from "@/lib/booking-flow";
import { addCalendarDays, dateInTimezone, displayDate, displaySlot } from "@/lib/date";
import type { AvailabilitySlot, Barber, BarberServiceAssignment, BookingSelection, Branch, CatalogService, TenantConfig } from "@/lib/types";
import { blankSelection } from "@/lib/types";
import { useCustomerSession } from "@/components/session";

type Step = "service" | "location" | "barber" | "time" | "details";
type Bootstrap = { config: TenantConfig; services: CatalogService[]; branches: Branch[]; barbers: Barber[]; assignments: BarberServiceAssignment[] };
type AvailabilityState = { loading: boolean; slots: AvailabilitySlot[]; error?: string };
const steps: { key: Step; label: string }[] = [
  { key: "service", label: "Service" }, { key: "location", label: "Location" }, { key: "barber", label: "Barber" }, { key: "time", label: "Time" }, { key: "details", label: "Details" },
];

function price(service: CatalogService): string { return `${service.price} ${service.currency}`; }

export default function BookingPage() {
  const router = useRouter();
  const [bootstrap, setBootstrap] = useState<Bootstrap>();
  const [selection, setSelection] = useState<BookingSelection>(blankSelection);
  const [step, setStep] = useState<Step>("service");
  const [availability, setAvailability] = useState<AvailabilityState>({ loading: false, slots: [] });
  const [loadError, setLoadError] = useState<unknown>();
  const [submitError, setSubmitError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [reloadAvailability, setReloadAvailability] = useState(0);
  const { customer } = useCustomerSession();
  const keyForRequest = useRef<{ fingerprint: string; key: string } | undefined>(undefined);

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const config = await apiClient.get<TenantConfig>("/v1/public/config", controller.signal);
        if (!isBookingConfig(config)) throw new ApiError(404);
        const [services, branches, barbers, assignments] = await Promise.all([
          apiClient.get<CatalogService[]>("/v1/public/services", controller.signal),
          apiClient.get<Branch[]>("/v1/public/branches", controller.signal),
          apiClient.get<Barber[]>("/v1/public/barbers", controller.signal),
          apiClient.get<BarberServiceAssignment[]>("/v1/public/barber-services", controller.signal),
        ]);
        if (!controller.signal.aborted) {
          setBootstrap({ config, services: services.filter((item) => item.active), branches: branches.filter((item) => item.active), barbers: barbers.filter((item) => item.active), assignments });
          setSelection((current) => current.date ? current : { ...current, date: dateInTimezone(config.business_timezone) });
        }
      } catch (error) {
        if (!controller.signal.aborted) setLoadError(error);
      }
    })();
    return () => controller.abort();
  }, []);

  useEffect(() => {
    if (!customer) return;
    setSelection((current) => ({
      ...current,
      customerName: current.customerName || customer.name,
	  customerEmail: current.customerEmail || customer.email,
    }));
  }, [customer]);

  const barbers = useMemo(() => eligibleBarbers(bootstrap?.barbers ?? [], selection.branchID, selection.serviceID, bootstrap?.assignments ?? []), [bootstrap?.barbers, bootstrap?.assignments, selection.branchID, selection.serviceID]);
  const selectedService = bootstrap?.services.find((item) => item.id === selection.serviceID);
  const selectedBranch = bootstrap?.branches.find((item) => item.id === selection.branchID);
  const selectedBarber = bootstrap?.barbers.find((item) => item.id === selection.barberID);
  const minDate = bootstrap ? dateInTimezone(bootstrap.config.business_timezone) : "";
  const maxDate = bootstrap ? addCalendarDays(minDate, bootstrap.config.settings.booking_horizon_days) : "";

  useEffect(() => {
    if (!selection.barberID || !selection.serviceID || !selection.date) {
      setAvailability({ loading: false, slots: [] });
      return;
    }
    const controller = new AbortController();
    setAvailability({ loading: true, slots: [] });
    const query = new URLSearchParams({ barber_id: selection.barberID, service_id: selection.serviceID, date: selection.date });
    void apiClient.get<AvailabilitySlot[]>(`/v1/public/availability?${query}`, controller.signal)
      .then((slots) => { if (!controller.signal.aborted) setAvailability({ loading: false, slots }); })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        const detail = error instanceof ApiError && error.status === 404 ? "This barber does not offer that service." : "Available times could not be loaded.";
        setAvailability({ loading: false, slots: [], error: detail });
      });
    return () => controller.abort();
  }, [selection.barberID, selection.serviceID, selection.date, reloadAvailability]);

  useEffect(() => {
    if (selection.barberID && !barbers.some((barber) => barber.id === selection.barberID)) {
      setSelection((current) => ({ ...current, barberID: "", startAt: "" }));
    }
  }, [barbers, selection.barberID]);

  function change(changeSet: Partial<BookingSelection>) {
    setSelection((current) => selectionAfterChange(current, changeSet, bootstrap?.barbers ?? [], bootstrap?.assignments ?? []));
    setSubmitError("");
  }

  function selectService(id: string) { change({ serviceID: id }); setStep("location"); }
  function selectBranch(id: string) { change({ branchID: id }); setStep("barber"); }
  function selectBarber(id: string) { change({ barberID: id }); setStep("time"); }
  function selectSlot(startAt: string) { change({ startAt }); setStep("details"); }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!selectedService || !selectedBranch || !selectedBarber || !selection.startAt || !validGuest(selection)) return;
    setSubmitting(true);
    setSubmitError("");
    keyForRequest.current = idempotencyForSelection(keyForRequest.current, selection);
    try {
      const appointment = await apiClient.post<{ id: string }>("/v1/public/appointments", {
        branch_id: selectedBranch.id,
        barber_id: selectedBarber.id,
        service_id: selectedService.id,
        customer_name: selection.customerName.trim(),
        customer_contact: selection.customerEmail.trim().toLowerCase(),
        start_at: selection.startAt,
      }, { headers: { "Idempotency-Key": keyForRequest.current.key } });
      router.push(`/success?appointment_id=${encodeURIComponent(appointment.id)}`);
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        change({ startAt: "" });
        setStep("time");
        setSubmitError("That time was just booked. Please choose another available time.");
        setReloadAvailability((value) => value + 1);
      } else {
        setSubmitError(error instanceof ApiError ? error.message : "Unable to complete the booking. Please try again.");
      }
    } finally {
      setSubmitting(false);
    }
  }

  if (loadError) return <Unavailable error={loadError} />;
  if (!bootstrap) return <Shell><main className="page centered"><div className="spinner" aria-label="Loading booking page" /><p>Loading booking details…</p></main></Shell>;

  const quickDates = Array.from({ length: 7 }, (_, index) => addCalendarDays(minDate, index));
  return <Shell businessName={bootstrap.config.business_name}>
    <main className="page">
      <header className="booking-header">
        <div><p className="eyebrow">Private appointment booking</p><h1>{bootstrap.config.business_name ?? "The studio"}</h1><p className="booking-kicker">Book your appointment</p></div>
        <div className="booking-location"><AccountLink /><span>Times in {bootstrap.config.business_timezone}</span></div>
      </header>
      <Progress step={step} selection={selection} onSelect={setStep} />
      <div className="booking-layout progressive-layout">
        <form className="booking-main" onSubmit={submit} noValidate>
          {step === "service" && <section className="booking-section active-step" aria-labelledby="service-heading"><SectionKicker label="01" /><div className="section-title"><h2 id="service-heading">Choose a service</h2><p>A considered menu for your visit.</p></div><div className="menu-list">{bootstrap.services.length === 0 ? <Empty text="No services are currently available." /> : bootstrap.services.map((service) => <button type="button" key={service.id} className={`menu-row ${selection.serviceID === service.id ? "selected" : ""}`} onClick={() => selectService(service.id)}><span><strong>{service.name}</strong><small>{service.duration_minutes} min</small></span><span className="menu-price">{price(service)}<i aria-hidden="true">{selection.serviceID === service.id ? "✓" : ""}</i></span></button>)}</div></section>}

          {step === "location" && <section className="booking-section active-step" aria-labelledby="branch-heading"><SectionKicker label="02" /><div className="section-title"><h2 id="branch-heading">Choose a location</h2><p>Choose the studio that suits you.</p></div><SelectionSummary service={selectedService} /><Back onClick={() => setStep("service")} /><div className="location-list">{bootstrap.branches.length === 0 ? <Empty text="No branches are currently available." /> : bootstrap.branches.map((branch) => <button type="button" key={branch.id} className={`location-row ${selection.branchID === branch.id ? "selected" : ""}`} onClick={() => selectBranch(branch.id)}><strong>{branch.name}</strong><span>{branch.address || "Studio location"}</span><i aria-hidden="true">{selection.branchID === branch.id ? "Selected" : ""}</i></button>)}</div></section>}

          {step === "barber" && <section className="booking-section active-step" aria-labelledby="barber-heading"><SectionKicker label="03" /><div className="section-title"><h2 id="barber-heading">Choose your barber</h2><p>Only barbers offering your selected service are shown.</p></div><SelectionSummary service={selectedService} branch={selectedBranch} /><Back onClick={() => setStep("location")} />{!selection.branchID ? <Empty text="Choose a location first." /> : <div className="barber-roster">{barbers.length === 0 ? <Empty text="No active barbers offer this service at this location." /> : barbers.map((barber) => <button type="button" key={barber.id} className={`barber-row ${selection.barberID === barber.id ? "selected" : ""}`} onClick={() => selectBarber(barber.id)}><span className="barber-mark" aria-hidden="true">{barber.display_name.slice(0, 1)}</span><span><strong>{barber.display_name}</strong><small>{barber.bio || "Barber"}</small></span><i aria-hidden="true">{selection.barberID === barber.id ? "✓" : ""}</i></button>)}</div>}</section>}

          {step === "time" && <section className="booking-section active-step time-section" aria-labelledby="date-heading"><SectionKicker label="04" /><div className="section-title"><h2 id="date-heading">Choose a date &amp; time</h2><p>Live availability, calculated for this studio.</p></div><SelectionSummary service={selectedService} branch={selectedBranch} barber={selectedBarber} /><Back onClick={() => setStep("barber")} />{!selection.serviceID || !selection.branchID || !selection.barberID ? <Empty text="Choose a service, location, and barber first." /> : <><div className="date-toolbar"><div className="date-strip" aria-label="Choose a date">{quickDates.map((date) => { const parts = dateParts(date); return <button type="button" key={date} className={`date-card ${selection.date === date ? "selected" : ""}`} onClick={() => change({ date })}><span>{parts.weekday}</span><strong>{parts.day}</strong><small>{parts.month}</small></button>; })}</div><label className="date-more" htmlFor="booking-date">Other date<input id="booking-date" type="date" min={minDate} max={maxDate} value={selection.date} onChange={(event) => change({ date: event.target.value })} /></label></div><div className="slots-heading"><span>Available times</span><small>{selection.date && displayDate(selection.date, bootstrap.config.business_timezone)}</small></div><div className="slots" aria-live="polite">{availability.loading && <p className="muted">Checking available times…</p>}{availability.error && <p className="error">{availability.error}</p>}{!availability.loading && !availability.error && availability.slots.length === 0 && <Empty text="No times available on this date. Try another day." />}{availability.slots.map((slot) => <button type="button" key={slot.start_at} className={`slot ${selection.startAt === slot.start_at ? "selected" : ""}`} onClick={() => selectSlot(slot.start_at)}>{displaySlot(slot.start_at, bootstrap.config.business_timezone)}</button>)}</div>{submitError && <p className="error" role="alert">{submitError}</p>}</>}</section>}

		  {step === "details" && <section className="booking-section active-step review" aria-labelledby="details-heading"><SectionKicker label="05" /><div className="section-title"><h2 id="details-heading">Your details</h2><p>Only what we need to hold your appointment.</p></div><SelectionSummary service={selectedService} branch={selectedBranch} barber={selectedBarber} startAt={selection.startAt} timezone={bootstrap.config.business_timezone} /><Back onClick={() => setStep("time")} /><div className="details"><label>Name<input value={selection.customerName} maxLength={200} autoComplete="name" required readOnly={Boolean(customer)} onChange={(event) => change({ customerName: event.target.value })} placeholder="Your full name" /></label><label>Email<input value={selection.customerEmail} type="email" autoComplete="email" required readOnly={Boolean(customer)} onChange={(event) => change({ customerEmail: event.target.value })} placeholder="you@example.com" aria-describedby="email-help" /></label><small id="email-help">{customer ? "Your saved customer details will be used for this booking." : "Your confirmation will be sent by email."}</small></div><div className="confirm-email">{selection.customerEmail && <>Confirmation to <strong>{selection.customerEmail.trim().toLowerCase()}</strong> by email</>}</div>{submitError && <p className="error" role="alert">{submitError}</p>}<button className="book-button" disabled={!validGuest(selection) || submitting}>{submitting ? "Confirming…" : "Confirm appointment"}</button></section>}
        </form>
        {step !== "service" && <aside className="booking-summary" aria-label="Appointment summary"><p className="eyebrow">Your appointment</p><SummaryDetails service={selectedService} branch={selectedBranch} barber={selectedBarber} startAt={selection.startAt} timezone={bootstrap.config.business_timezone} /><p className="summary-note">A confirmation email will be sent after booking.</p></aside>}
      </div>
    </main>
  </Shell>;
}

function Shell({ children, businessName }: { children: React.ReactNode; businessName?: string }) {
  return <div className="booking-shell">{children}<footer className="booking-footer"><div><strong>{businessName ?? "The studio"}</strong><span>© {new Date().getFullYear()} {businessName ?? "The studio"}</span></div><div><span>Powered by Barber Platform</span><span className="footer-links"><span>Privacy</span><span>Terms</span></span></div></footer></div>;
}

function Progress({ step, selection, onSelect }: { step: Step; selection: BookingSelection; onSelect: (step: Step) => void }) {
  const current = steps.findIndex((item) => item.key === step);
  const complete = [Boolean(selection.serviceID), Boolean(selection.branchID), Boolean(selection.barberID), Boolean(selection.startAt), validGuest(selection)];
  return <ol className="progress" aria-label="Booking progress">{steps.map((item, index) => <li className={`${index < current ? "complete" : ""} ${index === current ? "current" : ""}`} key={item.key}><button type="button" disabled={!complete[index] && index !== 0} onClick={() => index <= current && onSelect(item.key)}>{item.label}</button></li>)}</ol>;
}

function SectionKicker({ label }: { label: string }) { return <span className="section-kicker">{label}</span>; }
function AccountLink() { const { state } = useCustomerSession(); const signedIn = state === "authenticated"; return <Link className="account-link" href={signedIn ? "/account" : "/login"}>{signedIn ? "My account" : "Customer login"}</Link>; }
function Back({ onClick }: { onClick: () => void }) { return <button type="button" className="back-button" onClick={onClick}>← Back</button>; }

function SelectionSummary({ service, branch, barber, startAt, timezone }: { service?: CatalogService; branch?: Branch; barber?: Barber; startAt?: string; timezone?: string }) {
  const parts = [service?.name, branch?.name, barber?.display_name, startAt && timezone ? displaySlot(startAt, timezone) : undefined].filter(Boolean);
  return parts.length ? <p className="selection-summary">{parts.join(" · ")}{service && <small>{service.duration_minutes} min · {price(service)}</small>}</p> : null;
}

function dateParts(value: string) {
  const parts = new Intl.DateTimeFormat("en-US", { weekday: "short", day: "2-digit", month: "short", timeZone: "UTC" }).formatToParts(new Date(`${value}T12:00:00Z`));
  return { weekday: parts.find((part) => part.type === "weekday")?.value ?? "", day: parts.find((part) => part.type === "day")?.value ?? "", month: parts.find((part) => part.type === "month")?.value ?? "" };
}

function SummaryDetails({ service, branch, barber, startAt, timezone }: { service?: CatalogService; branch?: Branch; barber?: Barber; startAt: string; timezone: string }) {
  return <dl className="summary-details"><div><dt>Service</dt><dd>{service?.name ?? "—"}</dd></div><div><dt>Location</dt><dd>{branch?.name ?? "—"}</dd></div><div><dt>Barber</dt><dd>{barber?.display_name ?? "—"}</dd></div><div><dt>Date &amp; time</dt><dd>{startAt ? displaySlot(startAt, timezone) : "—"}</dd></div><div><dt>Price</dt><dd>{service ? price(service) : "—"}</dd></div></dl>;
}

function Empty({ text }: { text: string }) { return <p className="empty">{text}</p>; }
function Unavailable({ error }: { error: unknown }) {
  const unavailable = error instanceof ApiError && error.status === 404;
  return <Shell><main className="page centered"><section className="unavailable"><p className="eyebrow">Booking unavailable</p><h1>{unavailable ? "This booking page is not available" : "We could not load this booking page"}</h1><p>{unavailable ? "The domain may be inactive or no longer configured. Please contact the business directly." : "Please try again shortly or contact the business directly."}</p></section></main></Shell>;
}
