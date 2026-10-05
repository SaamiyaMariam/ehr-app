// Module 11 browser check: patient statements (open balance, date range, batch).
import {
  API, BASE, api, assert, createCharge, createClinician, createDiagnosis, createPatient, createPayer, createPolicy,
  createServiceCode, expectText, launch, login, must, recorder, shot, today, uid, userWithRoles,
} from "./lib.mjs";

const r = recorder("Module 11 — patient statements");

const biller = await userWithRoles(null, "M11 Biller", ["practice_biller", "clinician"]);
const clinician = await createClinician(biller.token, "M11 Clinician");
const payer = await createPayer(biller, `E2E M11 Payer ${uid()}`);
const service = await createServiceCode(biller, "100.00");

async function patientWithDirectCharges(label, dosList) {
  const patient = await createPatient(biller, `${label} ${uid()}`, clinician.id);
  for (const dos of dosList) {
    await must(
      api("POST", `/api/patients/${patient.id}/charges`, biller.token, {
        clinician_id: clinician.id, service_code_id: service.id, date_of_service: dos, units: 1, place_of_service: "11", billing_method: "direct",
      }),
    );
  }
  return patient;
}

const tag = uid("T");
const patient = await patientWithDirectCharges(`M11 Patient ${tag}`, [today(-40), today(-12)]); // $200 owed
const second = await patientWithDirectCharges(`M11 Second ${tag}`, [today(-5)]); // $100 owed
const nothing = await createPatient(biller, `M11 Nothing ${tag}`, clinician.id);

const { browser, context, page, problems } = await launch();

async function pdfBytes(path) {
  const response = await fetch(API + path, { headers: { Authorization: `Bearer ${biller.token}` } });
  assert(response.ok, `PDF request failed (${response.status})`);
  return Buffer.from(await response.arrayBuffer());
}

let first;

// The success message appears before the list reloads; wait for the rows.
async function waitForRows(count) {
  await page.waitForFunction((n) => document.querySelectorAll('[data-testid="statement-row"]').length === n, count, { timeout: 15000 });
}

try {
  await r.step("login and open Patient Billing → Statements", async () => {
    await login(page, biller);
    await page.goto(`${BASE}/patients/${patient.id}/billing`);
    await page.getByRole("link", { name: "Statements" }).click();
    await page.waitForURL(/billing\/statements$/);
    await expectText(page, "No statements have been generated");
  });

  await r.step("generate an open balance statement and open its PDF", async () => {
    await page.getByRole("button", { name: "Generate Open Balance Statement" }).click();
    await page.fill("#fd-comment", "Thank you for choosing our practice.");
    await page.getByRole("dialog").getByRole("button", { name: "Generate", exact: true }).click();
    await expectText(page, "generated");
    const row = page.getByTestId("statement-row").first();
    assert((await row.getByTestId("statement-balance").innerText()) === "$200.00", "balance at generation");

    const [popup] = await Promise.all([
      context.waitForEvent("page"),
      row.getByRole("button", { name: /Open PDF/ }).click(),
    ]);
    await popup.waitForURL(/^blob:/, { timeout: 15000 });
    await popup.close();

    const list = await must(api("GET", `/api/patients/${patient.id}/statements`, biller.token));
    first = list[0];
    const pdf = await pdfBytes(`/api/patients/${patient.id}/statements/${first.id}/pdf`);
    assert(pdf.subarray(0, 5).toString() === "%PDF-", "response is a PDF");
    await shot(page, "m11-open-balance-list");
  });

  await r.step("generate a date range statement", async () => {
    await page.getByRole("button", { name: "Generate Date Range Statement" }).click();
    await page.fill("#fd-start_date", today(-45));
    await page.fill("#fd-end_date", today(-20));
    await page.getByRole("dialog").getByRole("button", { name: "Generate", exact: true }).click();
    await expectText(page, "generated");
    await waitForRows(2);
    const rows = page.getByTestId("statement-row");
    // Range ends before the $100 service 12 days ago: only the 40-day-old charge counts.
    assert((await rows.first().getByTestId("statement-balance").innerText()) === "$100.00", "date-range balance as of the end date");
    await shot(page, "m11-date-range-list");
  });

  await r.step("post a payment afterwards; the old statement is unchanged", async () => {
    const before = await pdfBytes(`/api/patients/${patient.id}/statements/${first.id}/pdf`);
    await must(
      api("POST", `/api/patients/${patient.id}/payments`, biller.token, {
        payment_date: today(), amount: "150.00", method: "cash", idempotency_key: crypto.randomUUID(), auto_allocate: true,
      }),
    );
    const after = await pdfBytes(`/api/patients/${patient.id}/statements/${first.id}/pdf`);
    assert(Buffer.compare(before, after) === 0, "old statement PDF changed");

    await page.goto(`${BASE}/patients/${patient.id}/billing/statements`);
    await waitForRows(2);
    const rows = page.getByTestId("statement-row");
    const balances = await rows.getByTestId("statement-balance").allInnerTexts();
    assert(balances.includes("$200.00"), `old statement still shows $200.00, got ${balances.join(", ")}`);
  });

  await r.step("a new statement reflects the payment", async () => {
    await page.getByRole("button", { name: "Generate Open Balance Statement" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Generate", exact: true }).click();
    await expectText(page, "generated");
    await waitForRows(3);
    assert((await page.getByTestId("statement-row").first().getByTestId("statement-balance").innerText()) === "$50.00", "new balance");
  });

  await r.step("a patient with no balance cannot get a statement", async () => {
    const res = await api("POST", `/api/patients/${nothing.id}/statements`, biller.token, { statement_type: "open_balance" });
    assert(res.status === 409, `status ${res.status}`);
  });

  await r.step("Billing → Statements: filter, select patients, generate batch", async () => {
    await page.goto(`${BASE}/billing/statements`);
    await page.getByRole("link", { name: "Statements" }).first().waitFor();
    await page.fill("#bs-patient", tag);
    await page.selectOption("#bs-clinician", { index: 0 }).catch(() => {});
    await page.getByRole("button", { name: "Search" }).click();
    await page.getByTestId("candidate-row").first().waitFor();

    const names = await page.getByTestId("candidate-row").allInnerTexts();
    assert(names.some((n) => n.includes("M11 Patient")) && names.some((n) => n.includes("M11 Second")), "candidates listed");
    assert(!names.some((n) => n.includes("M11 Nothing")), "a patient without a balance must not be listed");

    // Bucket filter: only the 40-day-old $50 remainder of the first patient is in 31-60.
    await page.selectOption("#bs-bucket", "31-60");
    await page.getByRole("button", { name: "Search" }).click();
    await page.waitForTimeout(600);
    const aged = await page.getByTestId("candidate-row").allInnerTexts();
    assert(!aged.some((n) => n.includes("M11 Second")), "bucket filter should drop the recent-only patient");
    await page.selectOption("#bs-bucket", "");
    await page.getByRole("button", { name: "Search" }).click();
    await page.waitForTimeout(600);

    await page.getByLabel(new RegExp(`Select E2E M11 Patient ${tag}`)).check();
    await page.getByLabel(new RegExp(`Select E2E M11 Second ${tag}`)).check();
    await page.getByRole("button", { name: "Generate Open Balance Statements" }).click();
    await expectText(page, "2 statements generated");
    const created = await page.getByTestId("batch-created").locator("li").count();
    assert(created === 2, `created ${created}`);
    await shot(page, "m11-batch");
  });

  await r.step("combined batch PDF opens", async () => {
    const [popup] = await Promise.all([
      context.waitForEvent("page"),
      page.getByRole("button", { name: "Open Combined PDF" }).click(),
    ]);
    await popup.waitForURL(/^blob:/, { timeout: 15000 });
    await popup.close();
    const recent = await must(api("GET", "/api/billing/statements?page_size=2", biller.token));
    assert(recent.items.length === 2 && recent.items[0].batch_id && recent.items[0].batch_id === recent.items[1].batch_id, "batch statements share a batch id");
    assert(recent.items[0].patient_id !== recent.items[1].patient_id, "one statement per patient");
  });
} finally {
  await browser.close();
}

r.finish(problems);
