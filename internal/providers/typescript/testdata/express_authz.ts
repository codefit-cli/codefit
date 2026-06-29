// Express authorization surface: in Express the guard is MIDDLEWARE passed in the
// route registration (auth.required), not a call inside the handler body — so
// codefit must read the route's middleware arguments to set known_authz_detected,
// and phrase the signal honestly (route middleware, not the body).
import { Router, Request, Response } from 'express';
import auth from '../utils/auth';
import prisma from '../prisma/prisma-client';

const router = Router();

// Guarded by auth middleware in the route registration (not a body call).
router.post('/articles', auth.required, async (req: Request, res: Response) => {
  const article = await prisma.article.create({ data: req.body.article });
  res.json({ article });
});

// No authorization guard anywhere — neither middleware nor a body call.
router.delete('/articles/:slug', async (req: Request, res: Response) => {
  await prisma.article.delete({ where: { slug: req.params.slug } });
  res.sendStatus(204);
});

export default router;
