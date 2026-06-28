"use server";

import { db } from "@/lib/db";

// A Server Action that RETURNS a Prisma find with no select/omit. The framework
// serializes the return value to the client, so the over-fetch sink is the
// return statement (not Response.json). field_limiting_detected = false. No
// client id, so this is over-fetch (and authz) surface, not IDOR.
export async function listUsers() {
  return db.user.findMany();
}
