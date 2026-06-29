// Express over-fetching surface: the serialization sink is the response object's
// .json()/.send() (the handler's second parameter, `res`), not Response.json. A
// whole Prisma model serialized without select/omit is the over-fetch case; a
// field-limited find and a service-layer (frontier) source are also enumerated,
// each with its honest structural fact.
import { Router, Request, Response } from 'express';
import prisma from '../prisma/prisma-client';
import { getProfile } from '../services/profile.service';

const router = Router();

// Whole model, no select → field_limiting_detected=false, local_access_detected=true.
router.get('/users/:id', async (req: Request, res: Response) => {
  const user = await prisma.user.findUnique({ where: { id: Number(req.params.id) } });
  res.json(user);
});

// Field-limited find serialized via res.send → field_limiting_detected=true.
router.get('/users/:id/email', async (req: Request, res: Response) => {
  const user = await prisma.user.findUnique({ where: { id: Number(req.params.id) }, select: { email: true } });
  res.send(user);
});

// Serialized from a service call → frontier: local_access_detected=false.
router.get('/profiles/:username', async (req: Request, res: Response) => {
  const profile = await getProfile(req.params.username);
  res.json(profile);
});

export default router;
