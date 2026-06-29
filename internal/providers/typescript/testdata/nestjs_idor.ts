// NestJS controller: routes are class methods decorated with HTTP-verb decorators
// (@Get/@Delete), client inputs arrive via parameter decorators (@Param), and the
// resource access is either a local Prisma call (this.prisma.user.findUnique) or a
// delegation to a service in another file (this.usersService.remove → option C).
import { Controller, Get, Delete, Param } from '@nestjs/common';

@Controller('users')
export class UsersController {
  constructor(
    private readonly prisma: PrismaService,
    private readonly usersService: UsersService,
  ) {}

  // Local Prisma access reached by a client route param → IDOR (local).
  @Get(':id')
  async findOne(@Param('id') id: string) {
    return this.prisma.user.findUnique({ where: { id } });
  }

  // Delegates to a service in another file → IDOR (indirect, option C).
  @Delete(':id')
  async remove(@Param('id') id: string) {
    return this.usersService.remove(id);
  }

  // No client identifier → not an IDOR (negative; this is authz/over-fetch surface).
  @Get()
  async findAll() {
    return this.prisma.user.findMany();
  }
}
