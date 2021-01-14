# KDL

This contains work-in-progress stuff for KDL, the Kubernetes IDL.

You probably want to look at [some examples](./examples/testdata).

Otherwise, you might want [VIM syntax highlighting](./kdl.vim), if you're
opening examples in VIM.

There's also *technically* the WIP Go rewrite of the KDL compiler from its
initial prototype form (the prototype, which is not here, was written in
Rust).  You can find it [kdlc](./kdlc), and use it like

```shell
cd idl/kdlc; go build -o /tmp/kdlc .
cd idl/tocrd; go build -o /tmp/ir2crd .

# will print out a text form of the IR to stderr
cd $MYPROJECT
/tmp/kdlc . myapi.kdl > myapi.ckdl
/tmp/ir2crd group/version::Type myapi.ckdl
```

There may be bugs -- you've been warned ;-).

## A note on naming

There's also another project named KDL that happened just around when
I started working on this. It's pretty neat.  I'll probably rename this
soon to avoid confusion -- just haven't had time to rename everything yet
:-).

## Community, discussion, contribution, and support

Learn how to engage with the Kubernetes community on the [community page](http://kubernetes.io/community/).

You can reach the maintainers of this project at:

- [Slack](http://slack.k8s.io/)
- [Mailing List](https://groups.google.com/forum/#!forum/kubernetes-dev)

### Code of conduct

Participation in the Kubernetes community is governed by the [Kubernetes Code of Conduct](code-of-conduct.md).
