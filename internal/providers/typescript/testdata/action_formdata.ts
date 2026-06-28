"use server";

import { db } from "@/lib/db";

// A Server Action whose client input is a FormData (the progressive-enhancement
// form action). The id-input is read with formData.get("key"); the keys are
// named in the signal and the resulting access is local.
export async function updateName(formData: FormData) {
  const id = formData.get("id");
  await db.user.update({ where: { id }, data: { name: formData.get("name") } });
}
