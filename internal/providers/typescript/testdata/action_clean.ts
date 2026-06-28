"use server";

import { db } from "@/lib/db";
import { auth } from "@/lib/auth";

// A Server Action that calls a known authz helper in its body: known_authz
// detected = true. Still enumerated (presence, not judgment), but the fact lets
// the agent rank it below the unchecked ones.
export async function getInvoice(id: string) {
  const session = await auth();
  return db.invoice.findUnique({ where: { id, ownerId: session.user.id } });
}
