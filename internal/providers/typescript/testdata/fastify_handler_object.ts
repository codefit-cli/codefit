// Fastify routes in the OPTIONS-OBJECT form: the handler is an object property
// (handler: fn), not a positional argument, and the auth guard lives in the
// options object as preHandler/onRequest, not as a positional middleware. codefit
// must discover these, key inputs off the first handler param (request) and the
// response sink off the second (reply), and read preHandler as the authz guard.
import Fastify, { FastifyRequest, FastifyReply } from 'fastify';
import prisma from './prisma-client';
import auth from './auth';

const fastify = Fastify();

// Object-form, guarded by preHandler. Client route param → local Prisma access
// (IDOR surface); reply.send(user) serializes the whole model (over-fetch).
fastify.get('/users/:id', {
  preHandler: auth.required,
  handler: async (request: FastifyRequest, reply: FastifyReply) => {
    const user = await prisma.user.findUnique({ where: { id: Number(request.params.id) } });
    reply.send(user);
  },
});

// Object-form, NO guard, deletes by client id → IDOR with no authorization.
fastify.delete('/users/:id', {
  handler: async (request: FastifyRequest, reply: FastifyReply) => {
    await prisma.user.delete({ where: { id: Number(request.params.id) } });
    reply.send({ ok: true });
  },
});

export default fastify;
