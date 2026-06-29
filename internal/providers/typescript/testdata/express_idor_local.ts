// Express route handler with the resource access INLINE in the body (the local
// case): a client route param flows into a Prisma find in the same function.
// local_access_detected=true, no indirect_call.
import { Router, Request, Response } from 'express';
import prisma from '../prisma/prisma-client';

const router = Router();

router.get('/users/:id', async (req: Request, res: Response) => {
  const user = await prisma.user.findUnique({ where: { id: Number(req.params.id) } });
  res.json(user);
});

export default router;
