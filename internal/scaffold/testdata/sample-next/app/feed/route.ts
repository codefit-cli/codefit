import { UserService } from "@/lib/user-service";

// Serialized value comes from a service, not a local find → codefit cannot
// conclude locally (frontier_pending); the agent must follow it.
export async function GET() {
  return Response.json(await UserService.getAll());
}
