// Calls whose METHOD NAME matches an HTTP verb but that are NOT Express route
// handlers. The shape discriminator is: a route registration has a STRING PATH as
// its first argument AND a FUNCTION as its last argument. Each call below fails
// one of those, so none must be enumerated as surface. These are the false
// positives we deliberately do NOT mark (ADR 0005 — by shape, with an honest
// discriminator), kept here as explicit negatives rather than a mental note.
import { makeList } from './list';

const map = new Map<string, unknown>();
const value = 42;

// First arg is a string, but the last arg is a value, not a handler function.
map.get('/key', value);

const array = makeList();
const cb = (x: number) => x;

// Last arg is a function, but the first arg is not a string path.
array.get(0, cb);

// Single argument, no handler function at all.
cache.get('/users');
