# Edit and Delete Fix Plan

1. Update `renderAdminDashboard(editSlug string)` to take an optional `editSlug` parameter.
2. If `editSlug` is provided, fetch the plugin details from the database and populate the "Add Plugin" form (change it to "Edit Plugin" visually).
3. If `editSlug` is provided, add `<input type="hidden" name="original_slug" value="slug">`.
4. Add an `Edit` button next to `Remove`. `<form method="POST" action="/admin/plugin/marketplace-hub" enctype="multipart/form-data" style="display:inline;"><input type="hidden" name="action" value="edit_form"><input type="hidden" name="slug" value="slug"><button>Edit</button></form>`
5. Change `Remove` button to use `multipart/form-data` and hidden inputs.
6. In `handleAdminAction`, add `case "edit_form": return m.renderAdminDashboard(params["slug"])`.
7. Add `case "edit":` that updates the database instead of inserting new.
