import os
import glob
import re

theme_dir = "/home/ez8/gocms_local/gocms/themes/frontend/binarycms_quantum_light"
files = glob.glob(os.path.join(theme_dir, "*.html"))

css_pattern = re.compile(r"href=[\"']/uploads/quantum_light/quantum_light\.css(\?v=[0-9\.]+)?[\"']")
js_pattern = re.compile(r"src=[\"']/uploads/quantum_light/quantum_light\.js(\?v=[0-9\.]+)?[\"']")

for file in files:
    with open(file, "r") as f:
        content = f.read()

    original = content
    content = css_pattern.sub('href="{{ asset \\"/uploads/quantum_light/quantum_light.css\\" }}"', content)
    content = js_pattern.sub('src="{{ asset \\"/uploads/quantum_light/quantum_light.js\\" }}"', content)

    if original != content:
        with open(file, "w") as f:
            f.write(content)
        print(f"Patched {os.path.basename(file)}")

print("Asset wrapping completed.")
