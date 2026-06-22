import { readFile } from "fs/promises"

export async function loadUser(id: string, retries: number): Promise<string> {
  const path = `/users/${id}.json`
  return await readFile(path, "utf8")
}
