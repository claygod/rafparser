# rafparser

A high-performance Fujifilm RAW (.RAF) file parser and converter written in Go, featuring hardware-level pixel binning and professional HDR export to OpenEXR.

The core philosophy of this utility is to extract **the most honest, elastic, and uncompressed color information** directly from Fujifilm sensors (both X-Trans and Bayer), bypass traditional demosaicing interpolation, and avoid digital artifacts.

## ✨ Features

* **Pure Color Access:** Direct access to the sensor's raw linear coordinates without forced baked-in gamma curves.
* **Hardware Binning (2x2):** Combines pixel triplets at the physical level to achieve unparalleled micro-contrast and eliminate color noise.
* **HDR OpenEXR Export (.exr):** Saves image data in 16-bit floating-point (Half Float) format using lossless PIZ compression. No artificial clipping—highlights exceeding `1.0` are preserved and can be easily recovered via exposure adjustments in post-processing software.
* **Smart CLI Design:** Optimized for seamless integration into IDE run configurations. Cross-platform pathing handles slashes and directory validation natively on Linux, macOS, and Windows.

## ⚠️ Important Warning (Experimental Features)

The technical mosaic TIFF export (`-mosaic`), digital negative creation (`-dng`), and dual-pass highlight blending (`-recovery`) modules are currently **highly unstable**. Development on these features has just begun. They are provided strictly for reference and evaluation and are **not intended for production pipelines**. The `-exr`, `-tif`, and `-preview` flags are stable and fully production-ready.

## 🛠 Prerequisites & Requirements

This project relies on CGO bindings to the **LibRaw** library. Ensure that LibRaw development headers are installed on your host system before compiling.

### Debian/Ubuntu Setup:
```bash
sudo apt-get update
sudo apt-get install libraw-dev
```

### macOS Setup (via Homebrew):
```bash
brew install libraw
```

## 🚀 Installation & Compilation

1. Clone the repository:
   ```bash
   git clone https://github.com
   cd rafparser
   ```
2. Download Go dependencies (the pure Go OpenEXR module):
   ```bash
   go get ://github.com
   ```
3. Compile the utility:
   ```bash
   go build -o rafparser
   ```

## 💻 CLI Usage

The utility follows standard Linux command-line conventions. **The path to the input RAF file must always be passed as the very first argument.**

### Print Help Menu:
```bash
./rafparser --help
```

### Basic Syntax:
```bash
./rafparser <path_to_file.RAF> [options] [output_directory]
```
*If no output directory is specified, all generated files will be saved in the same folder as the input RAF file.*

### Examples:

1. **Generate HDR OpenEXR (Recommended Stable Workflow):**
   ```bash
   ./rafparser photo.RAF -exr
   ```
   *Creates `photo.RAF.exr` next to the source file, using a default light compression factor of 3.5 for standard tone mapping.*

2. **Batch Export Stable Formats into a Custom Directory:**
   ```bash
   ./rafparser photo.RAF -exr -tif -preview /home/user/output_folder
   ```
   *Generates an HDR EXR, a standard 16-bit linear TIFF, and extracts the embedded JPEG preview into the targeted directory.*

3. **Adjust EXR Light Value and Toggle Experimental Highlight Recovery:**
   ```bash
   ./rafparser photo.RAF -exr -light 4.2 -recovery
   ```
   *Runs dual-pass rendering for experimental highlight bleeding management and sets the midpoint linear exposure shift to 4.2.*

## 📋 Available Export Options

| Flag | Type | Description | Status |
| :--- | :--- | :--- | :--- |
| `-exr` | bool | Generate HDR OpenEXR file (`.raf.exr`) | **Stable** |
| `-tif` | bool | Generate standard linear TIFF frame (`.raf.tif`) | **Stable** |
| `-preview` | bool | Extract embedded high-quality JPEG preview (`.raf.preview.jpg`) | **Stable** |
| `-mosaic` | bool | Generate technical raw mosaic TIFF (`.raf.mosaic.tif`) | *Experimental* |
| `-dng` | bool | Generate Digital Negative Linear DNG (`.raf.dng`) | *Experimental* |
| `-recovery` | bool | Enable dual-pass extreme highlight management | *Experimental* |
| `-light` | float | Linear brightness reduction factor for EXR (default: `3.5`) | **Stable** |

## 📅 Roadmap & TODO

- [x] Implement stable 2x2 physical pixel hardware binning.
- [x] Full support for 16-bit float Linear OpenEXR with lossless PIZ compression.
- [x] Automatic cross-platform path calculation and IDE trailing argument catching.
- [x] Clean native shell interception for `--help` / `-h` instruction routines.
- [ ] Stabilize technical raw mosaic TIFF extraction (`-mosaic`).
- [ ] Rewrite binary tag layout structures to correctly assemble valid Linear DNG objects (`-dng`).
- [ ] Refactor the LERP/S-curve point calculation pipeline for highlight blending (`-recovery`).
- [ ] Write cross-compilation CI/CD automation build scripts.
- [ ] Create and link a dedicated repository hosting pre-compiled production binaries (Releases) for Windows and Linux users operating without a local Go toolchain.

## 📄 License

This project is licensed under the free software terms of the **GNU General Public License v3.0**. See the [LICENSE]file for details.

Copyright © 2026 Eduard Sesigin. All rights reserved. Contacts: claygod@yandex.ru

