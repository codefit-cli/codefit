import { db } from "@/lib/db";

// Inline (function-level) Server Action defined INSIDE a Server Component. The
// directive is the first statement of the function body, not the file. Detection
// is by shape, so this is enumerated even though the file is a page.tsx and the
// function is not exported.
export default function Page() {
  async function deleteItem(id: string) {
    "use server";
    await db.item.delete({ where: { id } });
  }
  return <button formAction={deleteItem}>Delete</button>;
}
