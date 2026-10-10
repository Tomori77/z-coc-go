from pathlib import Path

from PIL import Image


ROOT = Path(__file__).resolve().parents[1] / "assets" / "art"
SOURCES = [
    *sorted((ROOT / "generated").glob("*.png")),
    *sorted((ROOT / "user-edited").glob("*.png")),
    ROOT / "public-domain" / "loc-reading-room-1920.tif",
]


for source in SOURCES:
    if not source.exists():
        continue
    with Image.open(source) as image:
        if source.name == "loc-reading-room-1920.tif":
            image = image.crop((0, 0, image.width, image.height - 48))
        output = source.with_suffix(".webp")
        image.convert("RGB").save(output, "WEBP", quality=88, method=6)
        print(f"{source.name}: {image.width}x{image.height} -> {output.name}")
