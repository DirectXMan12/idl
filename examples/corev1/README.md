# Example: core/v1

This example includes several "conversions" of the core/v1 API group.

**TL;DR**: [types.manual.kdl](./types.manual.kdl) is what I'd expect
a somewhat-human-modified final version of core/v1 would look like.

- [types.ckdl.textpb](./types.ckdl.textpb): the textpb version of the CKDL ("compiled KDL") IR
  produced by running an experimental fork of controller-gen:
  `paths=./core/v1 output:dir=/tmp/idl-stuff/fromgo go2ir`.

  Eventually, this functionality in controller-gen will allow be the first
  step in migrating k/k and other existing projects to KDL.

  Note that normal CKDL files are current just proto instead ;-)

- [types.basic.kdl](./types.basic.kdl): the output of running `ir2idl < types.ckdl`.  This
  represents a kind of verbatim translation of existing Go to KDL.

- [types.nested.kdl](./types.nested.kdl): the output of running the `autonest | ir2idl
  < types.ckdl`.  This is naive autonested version of the KDL, nested
  according to least-common-ancestor.

- [types.manual.kdl](./types.manual.kdl): take `types.nested.kdl` and give it some human
  attention.  This is largely what I'd expect the final version of
  types.kdl to look like.
