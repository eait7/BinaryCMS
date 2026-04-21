package models

import (
	"github.com/ez8/gocms/internal/db"
)

type DashboardWidget struct {
	ID         int
	Title      string
	WidgetType string // "iframe", "builtin"
	SourceURL  string
	ColSpan    int // Bootstrap columns: 3, 4, 6, 12
	RowOrder   int
	Config     string
	Enabled    bool
}

func GetAllWidgets() ([]DashboardWidget, error) {
	rows, err := db.DB.Query("SELECT id, title, widget_type, source_url, col_span, row_order, config, enabled FROM dashboard_widgets ORDER BY row_order ASC, id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var widgets []DashboardWidget
	for rows.Next() {
		var w DashboardWidget
		var enabled int
		err := rows.Scan(&w.ID, &w.Title, &w.WidgetType, &w.SourceURL, &w.ColSpan, &w.RowOrder, &w.Config, &enabled)
		if err != nil {
			continue
		}
		w.Enabled = enabled == 1
		widgets = append(widgets, w)
	}
	return widgets, nil
}

func GetEnabledWidgets() ([]DashboardWidget, error) {
	rows, err := db.DB.Query("SELECT id, title, widget_type, source_url, col_span, row_order, config, enabled FROM dashboard_widgets WHERE enabled = 1 ORDER BY row_order ASC, id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var widgets []DashboardWidget
	for rows.Next() {
		var w DashboardWidget
		var enabled int
		err := rows.Scan(&w.ID, &w.Title, &w.WidgetType, &w.SourceURL, &w.ColSpan, &w.RowOrder, &w.Config, &enabled)
		if err != nil {
			continue
		}
		w.Enabled = enabled == 1
		widgets = append(widgets, w)
	}
	return widgets, nil
}

func GetWidgetByID(id int) (DashboardWidget, error) {
	var w DashboardWidget
	var enabled int
	err := db.DB.QueryRow("SELECT id, title, widget_type, source_url, col_span, row_order, config, enabled FROM dashboard_widgets WHERE id = ?", id).
		Scan(&w.ID, &w.Title, &w.WidgetType, &w.SourceURL, &w.ColSpan, &w.RowOrder, &w.Config, &enabled)
	w.Enabled = enabled == 1
	return w, err
}

func CreateWidget(w DashboardWidget) error {
	enabledInt := 0
	if w.Enabled {
		enabledInt = 1
	}
	_, err := db.DB.Exec(
		"INSERT INTO dashboard_widgets (title, widget_type, source_url, col_span, row_order, config, enabled) VALUES (?, ?, ?, ?, ?, ?, ?)",
		w.Title, w.WidgetType, w.SourceURL, w.ColSpan, w.RowOrder, w.Config, enabledInt,
	)
	return err
}

func UpdateWidget(w DashboardWidget) error {
	enabledInt := 0
	if w.Enabled {
		enabledInt = 1
	}
	_, err := db.DB.Exec(
		"UPDATE dashboard_widgets SET title = ?, widget_type = ?, source_url = ?, col_span = ?, row_order = ?, config = ?, enabled = ? WHERE id = ?",
		w.Title, w.WidgetType, w.SourceURL, w.ColSpan, w.RowOrder, w.Config, enabledInt, w.ID,
	)
	return err
}

func DeleteWidget(id int) error {
	_, err := db.DB.Exec("DELETE FROM dashboard_widgets WHERE id = ?", id)
	return err
}

func UpdateWidgetOrder(id int, order int) error {
	_, err := db.DB.Exec("UPDATE dashboard_widgets SET row_order = ? WHERE id = ?", order, id)
	return err
}
