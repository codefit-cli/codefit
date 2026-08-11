// Minimal per-language corpus item for "typescript"
// (internal/providers/registry/surfacecontract_test.go): a single Express route
// handler with a local Prisma access, guaranteed to produce at least one
// "authz" surface item through the real provider.
import { Router, Request, Response } from 'express';
import prisma from '../prisma/prisma-client';

const router = Router();

router.delete('/widgets/:id', async (req: Request, res: Response) => {
  await prisma.widget.delete({ where: { id: req.params.id } });
  res.sendStatus(204);
});

export default router;
