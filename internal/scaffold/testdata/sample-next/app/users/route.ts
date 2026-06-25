import { prisma } from "@/lib/prisma";

// Local Prisma find with NO select/omit → over-fetch with a gap (actionable).
export async function GET() {
  return Response.json(await prisma.user.findMany());
}
