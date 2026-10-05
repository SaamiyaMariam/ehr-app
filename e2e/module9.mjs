// Module 9 browser check: insurance payments / adjustments.
import {
  BASE, api, assert, createCharge, createClinician, createDiagnosis, createPatient, createPayer, createPolicy,
  createServiceCode, expectText, launch, login, logout, must, recorder, shot, submittedClaim, uid, userWithRoles, today,
} from "./lib.mjs";

const r = recorder("Module 9 — insurance payments");

// ---- Seed (API) -------------------------------------------------------
const owner = await userWithRoles(null, "M9 Owner", ["practice_biller", "clinician"]);
const biller = owner;
const scheduler = await userWithRoles(owner.token, "M9 Scheduler", ["practice_scheduler"]);
const clinician = await createClinician(owner.token, "M9 Clinician");

const payerName = `E2E M9 Payer ${uid()}`;
const payer = await createPayer(biller, payerName);
const service = await createServiceCode(biller, "100.00");
const patientLabel = `M9 Patient ${uid()}`;
const patient = await createPatient(biller, patientLabel, clinician.id);
const policy = await createPolicy(biller, patient.id, payer.id);
const diagnosis = await createDiagnosis(biller, patient.id);

const base = { clinicianId: clinician.id, serviceCodeId: service.id, policyId: policy.id, diagnosisId: diagnosis.id };

// charge1: patient owes $20, insurance $80.  charge2: insurance owes all $100.
const charge1 = await createCharge(biller, patient.id, { ...base, patientShare: "20.00" });
const charge2 = await createCharge(biller, patient.id, { ...base, patientShare: "0.00" });
const claim1 = await submittedClaim(biller, [charge1.id]);
const claim2 = await submittedClaim(biller, [charge2.id]);

console.log(`seeded patient ${patientLabel}, claims ${claim1.claim_number} / ${claim2.claim_number}`);

const { browser, page, problems } = await launch();

async function openEntryForm() {
  await page.goto(`${BASE}/billing`);
  await page.getByRole("link", { name: "Insurance Payments" }).click();
  await page.waitForURL(/\/billing\/insurance-payments$/);
  await page.getByRole("link", { name: "Enter Insurance Payment" }).click();
  await page.waitForURL(/insurance-payments\/new/);
}

async function selectPayerAndFind(claimNumber) {
  await page.selectOption("#ip-payer", { label: payerName });
  await page.fill("#out-claim", claimNumber);
  await page.getByRole("button", { name: "Find" }).click();
}

try {
  await r.step("biller logs in and opens Billing → Insurance Payments", async () => {
    await login(page, biller);
    await page.goto(`${BASE}/billing`);
    await page.getByRole("link", { name: "Insurance Payments" }).click();
    await page.waitForURL(/\/billing\/insurance-payments$/);
    await expectText(page, "Insurance Payments");
  });

  let partialPaymentUrl = "";

  await r.step("partial payment: select payer, find claim, allocate $50.00, save", async () => {
    await openEntryForm();
    await selectPayerAndFind(claim1.claim_number);

    await page.getByRole("button", { name: new RegExp(`Add ${claim1.claim_number} line 1`) }).click();
    await page.fill("#ip-amount", "50.00");
    await page.fill("#ip-reference", "CHK-M9-PARTIAL");

    const line = page.getByTestId("remit-line-0");
    await line.locator('input[id^="paid-"]').fill("50.00");
    await line.locator('input[id^="allowed-"]').fill("100.00");
    await line.getByRole("checkbox").uncheck(); // payer is not finished

    // Real-time summary helpers
    assert((await page.getByTestId("payment-total").innerText()) === "$50.00", "payment total");
    assert((await page.getByTestId("allocated-total").innerText()) === "$50.00", "allocated total");
    assert((await page.getByTestId("unallocated-total").innerText()) === "$0.00", "unallocated total");
    await shot(page, "m9-partial-entry");

    await page.getByRole("button", { name: "Post Insurance Payment" }).click();
    await page.waitForURL(/insurance-payments\/[0-9a-f-]{36}/);
    await expectText(page, "Insurance payment posted");
    partialPaymentUrl = page.url().split("?")[0];
  });

  await r.step("partial payment persists after refresh", async () => {
    await page.goto(partialPaymentUrl);
    await expectText(page, "Insurance payment of $50.00");
    await expectText(page, claim1.claim_number);
    await expectText(page, "CHK-M9-PARTIAL");
    await shot(page, "m9-partial-detail");
  });

  await r.step("claim 1 stays open with the payment shown in claim history/payments", async () => {
    await page.goto(`${BASE}/billing/claims/${claim1.id}`);
    await expectText(page, "Submitted externally");
    await expectText(page, "Insurance Payments");
    await expectText(page, "Payment Posted");
    const status = await must(api("GET", `/api/claims/${claim1.id}`, biller.token));
    assert(status.status === "externally_submitted", `claim status ${status.status}`);
    assert(status.lines[0].insurance_balance === "30.00", `insurance balance ${status.lines[0].insurance_balance}`);
  });

  await r.step("zero-dollar EOB: $70 contractual + $30 deductible to patient", async () => {
    await openEntryForm();
    await selectPayerAndFind(claim2.claim_number);
    await page.getByRole("button", { name: new RegExp(`Add ${claim2.claim_number} line 1`) }).click();

    await page.fill("#ip-amount", "0.00");
    await page.fill("#ip-reference", "EOB-M9-ZERO");

    const line = page.getByTestId("remit-line-0");
    await line.locator('input[id^="paid-"]').fill("0");
    await line.locator('input[id^="contractual-"]').fill("70.00");
    await line.locator('input[id^="deductible-"]').fill("30.00");

    await expectText(page, "Insurance balance after posting: $0.00");

    // A $0 payment must still be submittable.
    const submit = page.getByRole("button", { name: "Post Insurance Payment" });
    assert(await submit.isEnabled(), "submit must be enabled for a zero-dollar EOB");
    await shot(page, "m9-zero-eob-entry");

    await submit.click();
    await page.waitForURL(/insurance-payments\/[0-9a-f-]{36}/);
    await expectText(page, "Insurance payment of $0.00");
    await expectText(page, "Contractual Writeoff: $70.00");
    await expectText(page, "Deductible): $30.00");
    await page.reload();
    await expectText(page, "Insurance payment of $0.00");
  });

  await r.step("patient billing balances changed correctly (patient $50.00, insurance $30.00)", async () => {
    await page.goto(`${BASE}/patients/${patient.id}/billing`);
    await expectText(page, patientLabel);
    const charges = await must(api("GET", `/api/patients/${patient.id}/billing-transactions`, biller.token));
    const sum = (key) => charges.reduce((total, c) => total + Math.round(parseFloat(c.balances[key]) * 100), 0);
    assert(sum("patient_balance") === 5000, `patient balance ${sum("patient_balance")}`);
    assert(sum("insurance_balance") === 3000, `insurance balance ${sum("insurance_balance")}`);
    await shot(page, "m9-patient-billing");
  });

  await r.step("claim 2 resolved (paid)", async () => {
    await page.goto(`${BASE}/billing/claims/${claim2.id}`);
    await expectText(page, "Paid / adjudicated");
  });

  await r.step("post remaining resolution → claim 1 resolves", async () => {
    await openEntryForm();
    await selectPayerAndFind(claim1.claim_number);
    await page.getByRole("button", { name: new RegExp(`Add ${claim1.claim_number} line 1`) }).click();
    await page.fill("#ip-amount", "30.00");
    await page.fill("#ip-reference", "CHK-M9-FINAL");
    await page.getByTestId("remit-line-0").locator('input[id^="paid-"]').fill("30.00");
    await page.getByRole("button", { name: "Post Insurance Payment" }).click();
    await page.waitForURL(/insurance-payments\/[0-9a-f-]{36}/);

    await page.goto(`${BASE}/billing/claims/${claim1.id}`);
    await expectText(page, "Paid / adjudicated");
    await shot(page, "m9-claim-resolved");
  });

  await r.step("over-allocation is blocked in the UI and by the server", async () => {
    // Fresh claim to try to overpay.
    const charge3 = await createCharge(biller, patient.id, { ...base, patientShare: "0.00" });
    const claim3 = await submittedClaim(biller, [charge3.id]);

    await openEntryForm();
    await selectPayerAndFind(claim3.claim_number);
    await page.getByRole("button", { name: new RegExp(`Add ${claim3.claim_number} line 1`) }).click();
    await page.fill("#ip-amount", "50.00");
    await page.getByTestId("remit-line-0").locator('input[id^="paid-"]').fill("60.00");

    assert((await page.getByTestId("unallocated-total").innerText()) === "-$10.00", "unallocated shows overspend");
    await page.getByRole("button", { name: "Post Insurance Payment" }).click();
    await expectText(page, "exceed the payment");
    assert(/new$/.test(page.url().split("?")[0]), "stayed on the form");
  });

  await r.step("void the final payment → claim 1 reopens", async () => {
    await page.goto(`${BASE}/billing/insurance-payments`);
    await page.getByRole("link", { name: new RegExp(`^${today()}$`) }).first().waitFor();
    const list = await must(api("GET", "/api/insurance-payments?payer_id=" + payer.id, biller.token));
    const final = list.items.find((p) => p.reference_number === "CHK-M9-FINAL");
    await page.goto(`${BASE}/billing/insurance-payments/${final.id}`);
    await page.getByRole("button", { name: "Void Payment" }).click();
    await page.fill("#confirm-dialog-reason", "E2E void check");
    await page.getByRole("dialog").getByRole("button", { name: "Void Payment" }).click();
    await expectText(page, "Voided: E2E void check");

    const claim = await must(api("GET", `/api/claims/${claim1.id}`, biller.token));
    assert(claim.status === "externally_submitted", `claim status after void ${claim.status}`);
    assert(claim.lines[0].insurance_balance === "30.00", "balance restored");
  });

  await r.step("wrong-role user cannot post an insurance payment", async () => {
    await logout(page);
    await login(page, scheduler);
    await openEntryForm();
    await selectPayerAndFind(claim1.claim_number);
    await page.getByRole("button", { name: new RegExp(`Add ${claim1.claim_number} line 1`) }).click();
    await page.fill("#ip-amount", "10.00");
    await page.getByTestId("remit-line-0").locator('input[id^="paid-"]').fill("10.00");
    await page.getByTestId("remit-line-0").getByRole("checkbox").uncheck();
    await page.getByRole("button", { name: "Post Insurance Payment" }).click();
    await expectText(page, "do not have permission");

    const direct = await api("POST", "/api/insurance-payments", scheduler.token, {});
    assert(direct.status === 403, `direct API call status ${direct.status}`);
    const anonymous = await api("POST", "/api/insurance-payments", null, {});
    assert(anonymous.status === 401, `anonymous status ${anonymous.status}`);
  });
} finally {
  await browser.close();
}

r.finish(problems);
