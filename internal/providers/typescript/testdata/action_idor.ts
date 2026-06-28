"use server";

import { db } from "@/lib/db";

// File-level "use server": every exported async function is a Server Action.
// Receives a client-controlled id and deletes a resource with no authz check —
// the same IDOR shape as a route handler, but the input is the action argument.
export async function deleteInvoice(id: string) {
  await db.invoice.delete({ where: { id } });
}
