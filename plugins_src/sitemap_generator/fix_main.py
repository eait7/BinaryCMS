import re

with open("/home/ez8/.gemini/antigravity/scratch/binarycms/plugins_src/sitemap_generator/main.go", "r") as f:
    content = f.read()

# Fix the injected backticks
content = content.replace('` + "`" + `', '`')
content = content.replace('` + "`" + `', '`') # Just in case

# Fix the broken meta tag string formatting
old_meta = """		meta := fmt.Sprintf("
	<meta name="indexnow-key" content="%s" />
", key)"""
new_meta = """		meta := fmt.Sprintf("\\n\\t<meta name=\\"indexnow-key\\" content=\\"%s\\" />\\n", key)"""
content = content.replace(old_meta, new_meta)

# Fix the broken split
old_split = """	for _, pattern := range strings.Split(excludeStr, "
") {"""
new_split = """	for _, pattern := range strings.Split(excludeStr, "\\n") {"""
content = content.replace(old_split, new_split)

with open("/home/ez8/.gemini/antigravity/scratch/binarycms/plugins_src/sitemap_generator/main.go", "w") as f:
    f.write(content)
