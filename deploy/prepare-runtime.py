"""Fetch pinned public runtime code; never copy account state into an image."""
import hashlib
import http.client
import os
from pathlib import Path
import shutil
import sys
import tarfile
from urllib.request import urlopen

ROOT = Path(sys.argv[1])
LINUX_HASH = "ce260415598b78c0810ae6b917145fbca804f6f19385ca9d34d55921d38210cf"
PYPUSH = "f205e86bdf1a4ea5414a78985721c721039ada97"
ABSINTHE = "be8d3f83be30a88dca26ee7ad1fb862b8af889e9"
CACHE = Path(os.environ["APPLESERVICES_PUBLIC_CACHE"]) if os.environ.get("APPLESERVICES_PUBLIC_CACHE") else None


def cached(path, digest, algorithm="sha256"):
    if CACHE:
        entry = CACHE / digest
        if entry.is_file() and hashlib.new(algorithm, entry.read_bytes()).hexdigest() == digest:
            path.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(entry, path)
            return True
    return False


def save(path, data, digest):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_bytes(data)
    if CACHE:
        CACHE.mkdir(parents=True, exist_ok=True)
        (CACHE / digest).write_bytes(data)


def fetch(url, path, digest, algorithm="sha256"):
    if cached(path, digest, algorithm):
        return
    urls = [url]
    if url.startswith("https://raw.githubusercontent.com/"):
        owner, repo, ref, member = url.removeprefix("https://raw.githubusercontent.com/").split("/", 3)
        urls.append(f"https://cdn.jsdelivr.net/gh/{owner}/{repo}@{ref}/{member}")
    for source in urls * 2:
        try:
            with urlopen(source, timeout=25) as response:
                data = response.read()
        except (OSError, http.client.HTTPException):
            continue
        if hashlib.new(algorithm, data).hexdigest() != digest:
            raise RuntimeError("upstream runtime integrity check failed")
        save(path, data, digest)
        print(f"Verified public runtime: {path.name}", flush=True)
        return
    raise RuntimeError(f"public runtime download unavailable: {path.name}")


def main():
    library = ROOT / "openbubbles-linux/librust_lib_bluebubbles.so"
    url = "https://github.com/OpenBubbles/openbubbles-app/releases/download/v1.15.0%2B205/bluebubbles-linux-x86_64.tar"
    if not cached(library, LINUX_HASH):
        for attempt in range(3):
            try:
                with urlopen(url, timeout=45) as response:
                    with tarfile.open(fileobj=response, mode="r|") as archive:
                        for item in archive:
                            if item.name == "./lib/librust_lib_bluebubbles.so" and item.isfile() and item.size == 43342920:
                                data = archive.extractfile(item).read()
                                if hashlib.sha256(data).hexdigest() != LINUX_HASH:
                                    raise RuntimeError("native Linux module integrity check failed")
                                save(library, data, LINUX_HASH)
                                break
                        else:
                            raise RuntimeError("pinned Linux module missing from release")
                break
            except (OSError, http.client.HTTPException):
                if attempt == 2:
                    raise RuntimeError("official Linux module download unavailable") from None
    print("Verified public runtime: Linux ADI", flush=True)
    files = {
        "nac.py": "4690fadf1d46820d92fa28929478d5201bceb905820d07f8bc6d3a35efc343fc",
        "jelly.py": "8335f45e9971566e9803a14270e65aa7a1b8db083456930f17ca467e155cbeb8",
        "mparser.py": "c5365a6e225a9c69235e06aaad73e7b4a00c21292b4be3894c072607e7b4354c",
    }
    for name, digest in files.items():
        fetch(f"https://raw.githubusercontent.com/JJTech0130/pypush/{PYPUSH}/emulated/{name}",
              ROOT / "mac-nac-runtime/emulated" / name, digest)
    fetch("https://raw.githubusercontent.com/JJTech0130/nacserver/0a2aa2406542fb09c14a37db17ff14865b8aea29/IMDAppleServices",
          ROOT / "mac-nac-runtime/emulated/IMDAppleServices", "e1181ccad82e6629d52c6a006645ad87ee59bd13", "sha1")
    fetch(f"https://raw.githubusercontent.com/mosessupposeshis/imessage/{ABSINTHE}/rustpush/open-absinthe/src/asm/encrypt.s",
          ROOT / "activation-source-review/encrypt.s", "5f6bf2de1d6add2f98513a06b47e2fca0bd643026bee00a2d2ded531c31e73d8")
    print("Pinned Linux ADI and NAC runtime prepared (no account data)")


if __name__ == "__main__":
    main()
