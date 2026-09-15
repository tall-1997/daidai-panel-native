package com.daidai.daidai_app.data.model

import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class SystemOpsModelsTest {

    @Test fun `restore progress normalize handles go payload`() {
        val body = JSONObject().apply {
            put("data", JSONObject().apply {
                put("active", true)
                put("status", "running")
                put("percent", 45)
                put("file", "/tmp/backup.tar.gz")
                put("filename", "backup.tar.gz")
                put("stage", "unpacking")
                put("error", "")
            })
        }.toString()
        val p = RestoreProgress.fromJson(body)
        assertTrue(p.active)
        assertEquals("running", p.status)
        assertEquals(45, p.percent)
        assertEquals("backup.tar.gz", p.filename)
        assertEquals("unpacking", p.stage)
        assertFalse(p.finished)
    }

    @Test fun `restore progress finished states recognized`() {
        val body = JSONObject().apply {
            put("data", JSONObject().apply {
                put("active", false)
                put("status", "completed")
                put("percent", 100)
                put("filename", "")
                put("stage", "")
                put("error", "")
            })
        }.toString()
        val p = RestoreProgress.fromJson(body)
        assertTrue(p.finished)
        assertEquals("completed", p.status)
    }

    @Test fun `panel settings handles go object form`() {
        val body = JSONObject().apply {
            put("data", JSONObject().apply {
                put("panel_title", JSONObject().apply {
                    put("value", "My")
                    put("default_value", "DaiDai")
                })
                put("panel_icon", JSONObject().apply {
                    put("value", "")
                    put("default_value", "/static/icon.png")
                })
                put("editor_background_color", JSONObject().apply {
                    put("value", "#000")
                    put("default_value", "")
                })
            })
        }.toString()
        val s = PanelSettingsSnapshot.fromJson(body)
        assertEquals("My", s.panelTitle)
        assertEquals("/static/icon.png", s.panelIcon)
        assertEquals("#000", s.editorBackgroundColor)
    }

    @Test fun `panel settings handles local string form`() {
        val body = JSONObject().apply {
            put("data", JSONObject().apply {
                put("panel_title", "X")
                put("panel_icon", "")
                put("editor_background_color", "")
            })
        }.toString()
        val s = PanelSettingsSnapshot.fromJson(body)
        assertEquals("X", s.panelTitle)
        assertEquals("", s.panelIcon)
    }

    @Test fun `panel log page parses array response`() {
        val body = JSONObject().apply {
            put("data", JSONObject().apply {
                put("logs", JSONArray().apply {
                    put("line-1")
                    put("line-2")
                })
            })
        }.toString()
        val page = PanelLogPage.fromJson(body)
        assertEquals(2, page.lines.size)
        assertEquals("line-1", page.lines[0])
    }

    @Test fun `panel update status parse android immutable semantics`() {
        val body = JSONObject().apply {
            put("data", JSONObject().apply {
                put("status", "up_to_date")
                put("phase", "completed")
                put("message", "")
                put("deployment_type", "android_apk")
                put("update_manager", "monkeycode")
                put("update_lock", false)
            })
        }.toString()
        val status = PanelUpdateStatus.fromJson(body)
        assertTrue(status.isPlatformInstallerManaged)
        assertEquals("up_to_date", status.status)
        assertEquals("completed", status.phase)
        assertFalse(status.isRunning)
    }

    @Test fun `machine code fallback defaults`() {
        val body = JSONObject().apply {
            put("data", JSONObject().apply {
                put("code", "X-Y-Z")
            })
        }.toString()
        val code = MachineCode.fromJson(body)
        assertEquals("X-Y-Z", code.code)
        assertEquals("unknown", code.model)
        assertEquals("unknown", code.brand)
    }
}
