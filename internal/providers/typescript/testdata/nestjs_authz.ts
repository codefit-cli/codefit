// NestJS authorization: the guard is a @UseGuards decorator on the class
// (inherited by every method) or on the method. codefit detects it by PRESENCE
// (the decorator IS the guard mechanism) — guard class names are arbitrary, so it
// does not match them against a known set; it names the guard and reports
// known_authz_detected.
import { Controller, Get, Post, Param, UseGuards } from '@nestjs/common';

@Controller('admin')
@UseGuards(AuthGuard)
export class AdminController {
  constructor(private readonly prisma: PrismaService) {}

  // Inherits the class-level @UseGuards(AuthGuard) → guarded.
  @Get(':id')
  async findOne(@Param('id') id: string) {
    return this.prisma.account.findUnique({ where: { id } });
  }

  // Class guard plus a method-level guard.
  @Post()
  @UseGuards(RolesGuard)
  async create() {
    return this.prisma.account.create({ data: {} });
  }
}

@Controller('public')
export class PublicController {
  constructor(private readonly prisma: PrismaService) {}

  // No guard anywhere → unguarded.
  @Post()
  async create() {
    return this.prisma.note.create({ data: {} });
  }
}
