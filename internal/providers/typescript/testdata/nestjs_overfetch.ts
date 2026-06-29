// NestJS over-fetching: the handler returns the value and the framework serializes
// it (no res.json) — the return statement is the sink, exactly like a Server
// Action. A whole Prisma model is the over-fetch case; a field-limited find and a
// service-sourced (frontier) value are also enumerated with their honest facts.
import { Controller, Get } from '@nestjs/common';

@Controller('users')
export class UsersController {
  constructor(
    private readonly prisma: PrismaService,
    private readonly usersService: UsersService,
  ) {}

  // Whole model, no select → field_limiting_detected=false, local_access=true.
  @Get()
  async findAll() {
    return this.prisma.user.findMany();
  }

  // Field-limited find → field_limiting_detected=true.
  @Get('emails')
  async emails() {
    return this.prisma.user.findMany({ select: { email: true } });
  }

  // Serialized from a service call → frontier: local_access_detected=false.
  @Get('profiles')
  async profiles() {
    return this.usersService.findAll();
  }
}
