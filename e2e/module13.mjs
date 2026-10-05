// Module 13 browser check: secondary insurance lifecycle.
import {
  BASE, api, assert, assertCards, createCharge, createClinician, createDiagnosis, createPatient, createPayer, createPolicy,
  createServiceCode, expectText, launch, login, must, recorder, shot, submittedClaim, uid, userWithRoles,
} from "./lib.mjs";

const r = recorder("Module 13 — secondary insurance");

const biller = await userWithRoles(null, "M13 Biller", ["practice_biller", "clinician"]);
const scheduler = await userWithRoles(biller.token, "M13 Scheduler", ["practice_scheduler"]);
const clinician = await createClinician(biller.token, "M13 Clinician");

const primaryPayerName = `E2E M13 Primary Payer ${uid()}`;
const secondaryPayerName = `E2E M13 Secondary Payer ${uid()}`;
const primaryPayer = await createPayer(biller, primaryPayerName);
const secondaryPayer = await createPayer(biller, secondaryPayerName);
const service = await createServiceCode(biller, "100.00");
const patient = await createPatient(biller, `M13 Patient ${uid()}`, clinician.id);
const primaryPolicy = await createPolicy(biller, patient.id, primaryPayer.id, "primary");
const secondaryPolicy = await createPolicy(biller, patient.id, secondaryPayer.id, "secondary");
const diagnosis = await createDiagnosis(biller, patient.id);

const charge = await createCharge(biller, patient.id, {
  clinicianId: clinician.id, serviceCodeId: service.id, policyId: primaryPolicy.id, diagnosisId: diagnosis.id, patientShare: "0.00",
});
const primary = await submittedClaim(biller, [charge.id]);

const { browser, page, problems } = await launch();

async function postRemittanceUI(payerName, claimNumber, { amount, paid, deductible, reference }) {
  await page.goto(`${BASE}/billing/insurance-payments/new`);
  await page.selectOption("#ip-payer", { label: payerName });
  await page.fill("#out-claim", claimNumber);
  await page.getByRole("button", { name: "Find" }).click();
  await page.getByRole("button", { name: new RegExp(`Add ${claimNumber} line 1`) }).click();
  await page.fill("#ip-amount", amount);
  await page.fill("#ip-reference", reference);
  const line = page.getByTestId("remit-line-0");
  await line.locator('input[id^="paid-"]').fill(paid);
  if (deductible) await line.locator('input[id^="deductible-"]').fill(deductible);
  await page.getByRole("button", { name: "Post Insurance Payment" }).click();
  await page.waitForURL(/insurance-payments\/[0-9a-f-]{36}/);
}

let secondaryClaimId = "";
let secondaryNumber = "";

try {
  await r.step("login as biller", async () => {
    await login(page, biller);
  });

  await r.step("primary payer adjudicates: pays $50, $20 deductible to the patient", async () => {
    await postRemittanceUI(primaryPayerName, primary.claim_number, { amount: "50.00", paid: "50.00", deductible: "20.00", reference: "CHK-M13-PRIMARY" });
    await expectText(page, "Insurance payment posted");
  });

  await r.step("primary claim page shows Secondary Eligible with policy and amount", async () => {
    await page.goto(`${BASE}/billing/claims/${primary.id}`);
    const panel = page.getByTestId("next-insurance");
    await panel.waitFor();
    await expectText(page, "Eligible for Secondary Claim");
    assert((await page.getByTestId("next-eligible").innerText()).trim() === "$30.00", "eligible amount is the $30 left, not the $50 that was paid or the $20 moved to the patient");
    assert((await page.getByTestId("next-policy").innerText()).includes(secondaryPayerName), "secondary policy payer");
    await shot(page, "m13-eligible");
  });

  await r.step("create the secondary claim (explicit action)", async () => {
    await page.getByRole("button", { name: "Create Secondary Claim" }).click();
    // The primary claim's own URL has the same shape; wait for a different claim.
    await page.waitForURL((url) => /billing\/claims\/[0-9a-f-]{36}$/.test(url.pathname) && !url.pathname.endsWith(primary.id), { timeout: 15000 });
    await expectText(page, "Secondary");
    await expectText(page, secondaryPayerName);
    secondaryClaimId = page.url().split("/").pop();
    const claim = await must(api("GET", `/api/claims/${secondaryClaimId}`, biller.token));
    secondaryNumber = claim.claim_number;
    assert(claim.sequence === "secondary" && claim.payer_id === secondaryPayer.id && claim.insurance_policy_id === secondaryPolicy.id, "secondary payer / policy");
    assert(claim.previous_claim_id === primary.id, "linked to the primary claim");
  });

  await r.step("secondary claim preserves the primary adjudication snapshot", async () => {
    const panel = page.getByTestId("earlier-payer");
    await panel.waitFor();
    const text = await panel.innerText();
    for (const expected of [primary.claim_number, "$100.00", "$50.00", "$20.00", "$30.00"]) {
      assert(text.includes(expected), `earlier payer panel is missing ${expected}`);
    }
    await shot(page, "m13-secondary-claim");
  });

  await r.step("submit the secondary claim externally", async () => {
    await expectText(page, "Ready");
    await page.getByRole("button", { name: "Mark Submitted Externally" }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Mark Submitted" }).click();
    await expectText(page, "Submitted externally");
  });

  await r.step("no duplicate: primary shows the secondary claim and cannot create another", async () => {
    await page.goto(`${BASE}/billing/claims/${primary.id}`);
    await page.getByTestId("next-insurance").waitFor();
    await expectText(page, "Secondary claim exists");
    assert(await page.getByRole("button", { name: "Create Secondary Claim" }).isDisabled(), "create button must be disabled");
    await expectText(page, "Follow-on claims");
    await expectText(page, secondaryNumber);
    const again = await api("POST", `/api/claims/${primary.id}/next-sequence`, biller.token, {});
    assert(again.status === 409, `second creation returned ${again.status}`);
  });

  await r.step("post the secondary payment: only the secondary claim and shared balance change", async () => {
    await postRemittanceUI(secondaryPayerName, secondaryNumber, { amount: "30.00", paid: "30.00", reference: "CHK-M13-SECONDARY" });
    const sec = await must(api("GET", `/api/claims/${secondaryClaimId}`, biller.token));
    const pri = await must(api("GET", `/api/claims/${primary.id}`, biller.token));
    assert(sec.status === "paid", `secondary status ${sec.status}`);
    assert(pri.status === "paid", `primary status ${pri.status}`);
    assert(sec.lines[0].insurance_paid === "30.00" && pri.lines[0].insurance_paid === "50.00", "each claim shows only its own payer's payment");
  });

  await r.step("balances: patient $20.00 (deductible), insurance $0.00, nothing double counted", async () => {
    await assertCards(page, patient.id, { patient: "$20.00", insurance: "$0.00", total: "$20.00", credit: "$0.00" }, "after secondary payment");
    const summary = await must(api("GET", `/api/patients/${patient.id}/billing-summary`, biller.token));
    assert(summary.total_outstanding === "20.00", `total ${summary.total_outstanding}`);
    const claims = await must(api("GET", `/api/claims?patient_id=${patient.id}&sequence=secondary`, biller.token));
    assert(claims.total === 1, `${claims.total} secondary claims`);
    await shot(page, "m13-balances");
  });

  await r.step("a paid-in-full claim offers no further insurance", async () => {
    await page.goto(`${BASE}/billing/claims/${secondaryClaimId}`);
    await page.getByTestId("next-insurance").waitFor();
    await expectText(page, "Tertiary Insurance");
    await expectText(page, "No remaining insurance responsibility");
    assert(await page.getByRole("button", { name: "Create Tertiary Claim" }).isDisabled(), "tertiary button disabled");
  });

  await r.step("wrong-role user cannot create a follow-on claim", async () => {
    const res = await api("POST", `/api/claims/${primary.id}/next-sequence`, scheduler.token, {});
    assert(res.status === 403, `scheduler status ${res.status}`);
    const anonymous = await api("POST", `/api/claims/${primary.id}/next-sequence`, null, {});
    assert(anonymous.status === 401, `anonymous status ${anonymous.status}`);
  });
} finally {
  await browser.close();
}

r.finish(problems);
