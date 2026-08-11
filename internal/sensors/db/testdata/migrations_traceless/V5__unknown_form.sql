-- STATE (a) — GENUINE BLINDNESS through a form NO branch reads.
--
-- CREATE DOMAIN reaches the reducer's residual `default:` branch, which is the
-- same branch every DML statement used to reach. This file is the control that
-- proves the DML/permission recognition did NOT swallow the residual bucket: if
-- "declares no schema" were ever inferred from `default:` instead of from a
-- positive head match, this file would be reported as benign and the original
-- lie would come back wearing a friendlier sentence.
CREATE DOMAIN email_addr AS text;
