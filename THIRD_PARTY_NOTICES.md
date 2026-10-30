# Third-Party Notices

This repository contains code originating from separately licensed projects. Those components remain under their original licenses. The PolyForm Noncommercial License in the repository root applies only to copyrightable work that the repository licensor has the right to license under those terms; it does not replace or restrict rights already granted by third-party licenses.

## Original appleservices source

Portions derived from the original source are available under the following MIT License:

```text
MIT License

Copyright (c) 2026 Lauren

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

## SAP implementation

The code under `internal/sap/` includes its own MIT license and copyright notice in [`internal/sap/LICENSE`](internal/sap/LICENSE). That license continues to govern the corresponding component.

Additional dependencies may carry their own license files or notices. Nothing in the repository's top-level license removes rights or obligations under those separate licenses.

## Optional container runtime

The container build fetches the OpenBubbles `v1.15.0+205` Linux native module from its official release, verified against SHA256 `ce260415598b78c0810ae6b917145fbca804f6f19385ca9d34d55921d38210cf`. Its closed-source ADI implementation is not replaced by the public ClearADI stub, nor relicensed by this repository. No native module is committed here.

The build also fetches pinned NAC tooling from [JJTech0130/pypush](https://github.com/JJTech0130/pypush/tree/f205e86bdf1a4ea5414a78985721c721039ada97/emulated), the reference `IMDAppleServices` image from [JJTech0130/nacserver](https://github.com/JJTech0130/nacserver/tree/0a2aa2406542fb09c14a37db17ff14865b8aea29), and the XNU property routine and conformance vectors from [OpenAbsinthe](https://github.com/mosessupposeshis/imessage/tree/be8d3f83be30a88dca26ee7ad1fb862b8af889e9/rustpush/open-absinthe). These externally fetched components retain their respective rights and licensing; the repository's license does not grant rights to Apple's binaries. No upstream hardware profile or account state is included.

Python packages installed in the runtime retain their license metadata in their distribution directories. QEMU and its distribution copyright notices are installed by Debian's package manager. Go dependencies remain under their own module licenses.
