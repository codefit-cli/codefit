"use server";

import { db } from "@/lib/db";

// Edge case: the client-controlled id arrives NESTED inside an object argument
// (data.id), not as a flat primitive. Seeding the parameter binding `data` as the
// id-var means data.id flows to the local Prisma access, so this still enumerates
// as IDOR. Proves object-shaped action args are covered, not a blind spot.
export async function updateInvoice(data: { id: string; amount: number }) {
  await db.invoice.update({ where: { id: data.id }, data: { amount: data.amount } });
}
