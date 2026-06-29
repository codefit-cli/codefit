// Express controller that reaches the resource through a service in another file
// (the cross-file option C shape, mirrored from the RealWorld dogfood). codefit
// sees the client route param and the service call in THIS file; it must emit
// indirect_access=true + indirect_call naming the callee, without opening the
// service. Neither handler checks that the caller owns the article — the two
// confirmed IDORs.
import { Router, Request, Response, NextFunction } from 'express';
import { updateArticle, deleteArticle } from '../services/article.service';
import auth from '../utils/auth';

const router = Router();

router.put(
  '/articles/:slug',
  auth.required,
  async (req: Request, res: Response, next: NextFunction) => {
    try {
      const article = await updateArticle(req.body.article, req.params.slug, req.user?.username as string);
      res.json({ article });
    } catch (error) {
      next(error);
    }
  },
);

router.delete('/articles/:slug', auth.required, async (req: Request, res: Response, next: NextFunction) => {
  try {
    await deleteArticle(req.params.slug);
    res.sendStatus(204);
  } catch (error) {
    next(error);
  }
});

export default router;
