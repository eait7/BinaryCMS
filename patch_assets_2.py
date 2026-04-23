import os
import glob
import re

theme_dir = "/home/ez8/gocms_local/gocms/themes/frontend/binarycms_quantum_light"
files = glob.glob(os.path.join(theme_dir, "*.html"))

css_pattern = re.compile(r"\{\{ asset \\\"/uploads/(.*?)\\\" \}\}")

for file in files:
    with open(file, "r") as f:
        content = f.read()

    original = content
    content = css_pattern.sub(r'{{ asset "/uploads/\1" }}', content)

    if original != content:
        with open(file, "w") as f:
            f.write(content)
        print(f"Patched {os.path.basename(file)}")

print("Asset wrapping quote fix completed.")
