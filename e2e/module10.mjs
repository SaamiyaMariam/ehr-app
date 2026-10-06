// Module 10 browser check: balance engine (summary cards, ledger, breakdown).
import {
  API, BASE, assert, assertCards, createCharge, createClinician, createDiagnosis, createPatient, createPayer, createPolicy,
  createServiceCode, expectText, launch, login, recorder, shot, submittedClaim, uid, userWithRoles, api, must,
} from "./lib.mjs";

const r = recorder("Module 10 — balance engine");

const biller = await userWithRoles(null, "M10 Biller", ["practice_biller", "clinician"]);
const clinician = await createClinician(biller.token, "M10 Clinician");
const payerName = `E2E M10 Payer ${uid()}`;
const payer = await createPayer(biller, payerName);
const service = await createServiceCode(biller, "100.00");
const patient = await createPatient(biller, `M10 Patient ${uid()}`, clinician.id);
const policy = await createPolicy(biller, patient.id, payer.id);
const diagnosis = await createDiagnosis(biller, patient.id);
const charge = await createCharge(biller, patient.id, {
  clinicianId: clinician.id, serviceCodeId: service.id, policyId: policy.id, diagnosisId: diagnosis.id, patientShare: "20.00",
});
const claim = await submittedClaim(biller, [charge.id]);

const { browser, page, problems } = await launch();
const chargeUrl = `${BASE}/patients/${patient.id}/billing/charges/${charge.id}`;

async function patientPaysUI(amount) {
  await page.goto(`${BASE}/patients/${patient.id}/billing/payments/new`);
  await page.fill("#pay-amount", amount);
  await page.getByRole("button", { name: "Post Payment" }).click();
  await page.waitForURL(/billing\/payments\/[0-9a-f-]{36}/);
  return page.url().split("/").pop();
}

try {
  await r.step("login as biller", async () => {
    await login(page, biller);
  });

  await r.step("charge with insurance split: patient $20, insurance $80", async () => {
    await assertCards(page, patient.id, { patient: "$20.00", insurance: "$80.00", total: "$100.00", credit: "$0.00" }, "initial");
    await shot(page, "m10-initial");
  });

  await r.step("patient payment → patient balance drops, insurance unchanged", async () => {
    await patientPaysUI("20.00");
    await assertCards(page, patient.id, { patient: "$0.00", insurance: "$80.00", total: "$80.00", credit: "$0.00" }, "after patient payment");
  });

  await r.step("insurance payment (partial $30) → insurance balance drops, patient unchanged", async () => {
    await page.goto(`${BASE}/billing/insurance-payments/new`);
    await page.selectOption("#ip-payer", { label: payerName });
    await page.fill("#out-claim", claim.claim_number);
    await page.getByRole("button", { name: "Find" }).click();
    await page.getByRole("button", { name: new RegExp(`Add ${claim.claim_number} line 1`) }).click();
    await page.fill("#ip-amount", "30.00");
    const line = page.getByTestId("remit-line-0");
    await line.locator('input[id^="paid-"]').fill("30.00");
    await line.getByRole("checkbox").uncheck();
    await page.getByRole("button", { name: "Post Insurance Payment" }).click();
    await page.waitForURL(/insurance-payments\/[0-9a-f-]{36}/);
    await assertCards(page, patient.id, { patient: "$0.00", insurance: "$50.00", total: "$50.00", credit: "$0.00" }, "after insurance payment");
  });

  await r.step("transfer responsibility insurance → patient: insurance down, patient up, total unchanged", async () => {
    await page.goto(chargeUrl);
    await page.getByRole("button", { name: "Transfer Responsibility" }).click();
    await page.fill("#fd-amount", "20.00");
    await page.selectOption("#fd-reason", "deductible");
    await page.getByRole("dialog").getByRole("button", { name: "Transfer", exact: true }).click();
    await expectText(page, "Responsibility transferred");
    await assertCards(page, patient.id, { patient: "$20.00", insurance: "$30.00", total: "$50.00", credit: "$0.00" }, "after transfer");
  });

  await r.step("charge page explains the balance (breakdown + events)", async () => {
    await page.goto(chargeUrl);
    const table = page.getByTestId("breakdown");
    await table.waitFor();
    const text = await table.innerText();
    for (const expected of ["Responsibility at charge", "Moved to patient", "Current responsibility", "Payments", "Balance"]) {
      assert(text.includes(expected), `breakdown is missing "${expected}"`);
    }
    await expectText(page, "Responsibility moved here");
    await shot(page, "m10-breakdown");
  });

  await r.step("refund: pay, unapply, refund the credit → balance restored, credit gone", async () => {
    const paymentId = await patientPaysUI("20.00");
    await assertCards(page, patient.id, { patient: "$0.00", insurance: "$30.00", total: "$30.00", credit: "$0.00" }, "after second patient payment");

    // Unapply the payment: the service is owed again and the money is credit.
    await page.goto(`${BASE}/patients/${patient.id}/billing/payments/${paymentId}`);
    await page.getByRole("button", { name: "Unapply" }).first().click();
    await expectText(page, "Allocation removed");
    await assertCards(page, patient.id, { patient: "$20.00", insurance: "$30.00", total: "$50.00", credit: "$20.00" }, "after unapply");

    await page.goto(`${BASE}/patients/${patient.id}/billing/payments/${paymentId}`);
    await page.getByRole("button", { name: "Refund Credit" }).click();
    await page.fill("#fd-reason", "E2E refund");
    await page.getByRole("dialog").getByRole("button", { name: "Record Refund" }).click();
    await expectText(page, "Refund recorded");
    await assertCards(page, patient.id, { patient: "$20.00", insurance: "$30.00", total: "$50.00", credit: "$0.00" }, "after refund");
    await shot(page, "m10-after-refund");
  });

  await r.step("void the transfer on the charge page → responsibility moves back", async () => {
    await page.goto(chargeUrl);
    await page.getByRole("button", { name: "Void", exact: true }).first().click();
    await page.fill("#confirm-dialog-reason", "E2E reverse transfer");
    await page.getByRole("dialog").getByRole("button", { name: "Void", exact: true }).click();
    await expectText(page, "Entry voided");
    await assertCards(page, patient.id, { patient: "$0.00", insurance: "$50.00", total: "$50.00", credit: "$0.00" }, "after voiding the transfer");
  });

  await r.step("ledger lists every event and can include voided ones", async () => {
    await page.goto(`${BASE}/patients/${patient.id}/billing`);
    await page.getByRole("button", { name: "Patient", exact: true }).click();
    await expectText(page, "Patient payment applied");
    await page.getByLabel("Show voided").check();
    await expectText(page, "Voided");
    await shot(page, "m10-ledger");
  });

  await r.step("API reconciles: summary equals the sum of charge balances", async () => {
    const summary = await must(api("GET", `/api/patients/${patient.id}/billing-summary`, biller.token));
    const charges = await must(api("GET", `/api/patients/${patient.id}/billing-transactions`, biller.token));
    const cents = (v) => Math.round(parseFloat(v) * 100);
    const patientSum = charges.reduce((s, c) => s + cents(c.balances.patient_balance), 0);
    const insuranceSum = charges.reduce((s, c) => s + cents(c.balances.insurance_balance), 0);
    assert(cents(summary.patient_balance) === patientSum && cents(summary.insurance_balance) === insuranceSum, "summary differs from charge balances");
    void API;
  });
} finally {
  await browser.close();
}

r.finish(problems);
