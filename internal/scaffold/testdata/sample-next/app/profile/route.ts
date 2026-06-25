import { prisma } from "@/lib/prisma";
import { getServerSession } from "next-auth";

// Authorization check present (getServerSession) AND field limiting present
// (select) → codefit finds the expected controls locally, no gap (resolved_clean).
export async function GET() {
  const session = await getServerSession();
  if (!session) {
    return new Response("Unauthorized", { status: 401 });
  }
  return Response.json(
    await prisma.user.findFirst({ select: { id: true, name: true } }),
  );
}
