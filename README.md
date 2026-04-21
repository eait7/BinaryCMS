# BinaryCMS

[Visit BinaryCMS.com](https://binarycms.com) for official documentation and details.

A lightweight, lightning-fast Content Management System (CMS) built purely in Go (Golang). It features a completely modular, dynamic plugin-first architecture for ultimate extensibility without altering core code.

## 🌟 Key Features

- **Blazing Fast**: Compiles to a single binary. No PHP, no complex runtime.
- **Plugin-First Architecture**: Extend almost any part of the system (admin routes, dashboard widgets, backend menus, frontend pages, user profiles, hooks) with standalone Go plugins using the `hashicorp/go-plugin` ecosystem.
- **Dynamic User Management**: A complete frontend and backend user flow built out-of-the-box, letting plugins store data safely in the `user_meta` key-value table. 
- **Theming System**: Drag-and-drop HTML templating. Total separation of logic and presentation.

## 🚀 One-Command Install

To get BinaryCMS running instantly on your Linux/macOS machine, run this single command:

```bash
curl -sSL https://raw.githubusercontent.com/eait7/BinaryCMS/main/install.sh | bash
```

This will automatically:
1. Clone the repository.
2. Compile the core `gocms_server` binary.
3. Compile all default bundled plugins (`visit_tracker`, `advanced_analytics`).
4. Start the server on port `8080`.

## 🛠️ Manual Installation (For Developers)

If you'd like to manually tinker with the code:

### Prerequisites
- [Go](https://golang.org/doc/install) 1.21+ 

### Steps
1. **Clone the Repo**
   ```bash
   git clone https://github.com/eait7/BinaryCMS.git
   cd BinaryCMS
   ```

2. **Build the Core Server**
   ```bash
   go build -o gocms_server ./cmd/server
   ```

3. **Build the Plugins**
   BinaryCMS relies on its plugins. Build them into the `plugins/` directory:
   ```bash
   # Build the View Tracker
   cd plugins_src/visit_tracker && go build -o ../../plugins/visit_tracker && cd ../..
   
   # Build the Advanced Analytics 
   cd plugins_src/advanced_analytics && go build -o ../../plugins/advanced_analytics && cd ../..
   ```

4. **Run the Server**
   ```bash
   ./gocms_server
   ```
   Navigate to `http://localhost:8080` to see your running CMS!
   The Admin dashboard is at `http://localhost:8080/admin` (Default: admin/admin).

## 🧩 Building Plugins

Plugins in BinaryCMS interact with the core via standard Go interfaces wrapped in RPC.

Check out the built-in **Plugin Developer Guide** right inside your admin portal via `http://localhost:8080/admin/users/developer-guide` for comprehensive documentation on user hooks, meta fields, and dashboard widget injections! 

## License
MIT License
