-- The ONLY file in this corpus that contributes a position to the model.
-- Every other migration here leaves no trace, for a DIFFERENT reason each time;
-- V1 is what makes those reasons distinguishable at all.
CREATE TABLE _user (
    id                    bigserial PRIMARY KEY,
    email                 text NOT NULL,
    license_status        text,
    license_expires_at    timestamptz
);

CREATE TABLE role (
    id    bigserial PRIMARY KEY,
    name  text NOT NULL
);
