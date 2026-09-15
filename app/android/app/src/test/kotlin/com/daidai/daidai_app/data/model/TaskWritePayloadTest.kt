package com.daidai.daidai_app.data.model

import org.json.JSONObject
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

/**
 * TaskWritePayload 序列化测试（F4：高级字段 JSON key 对齐 Go 后端 task_mutate.go
 * 与本地 LocalPanelStore.createTask/updateTask）。
 */
class TaskWritePayloadTest {

    @Test fun `basic fields serialize with backend keys`() {
        val json = TaskWritePayload(
            name = "sync",
            scriptPath = "/scripts/sync.sh",
            schedule = "0 0 * * *",
            type = "cron",
        ).toJson()

        assertEquals("sync", json.getString("name"))
        assertEquals("/scripts/sync.sh", json.getString("command"))
        assertEquals("0 0 * * *", json.getString("cron_expression"))
        assertEquals("cron", json.getString("task_type"))
        assertEquals(1, json.getInt("status"))
    }

    @Test fun `advanced fields serialize with backend keys`() {
        val json = TaskWritePayload(
            name = "daily",
            scriptPath = "python /app/job.py",
            schedule = "0 0 * * *",
            timeout = 300,
            maxRetries = 3,
            retryInterval = 60,
            stopSchedule = "0 23 * * *",
            dependsOn = 7L,
            taskBefore = "/scripts/pre.sh",
            taskAfter = "/scripts/post.sh",
            allowMultipleInstances = true,
            schedulePolicy = "queue",
            pythonVersion = "3.11",
            notifyOnFailure = true,
            notifyOnSuccess = false,
            notifyOnAbort = true,
        ).toJson()

        assertEquals(300, json.getInt("timeout"))
        assertEquals(3, json.getInt("max_retries"))
        assertEquals(60, json.getInt("retry_interval"))
        assertEquals("0 23 * * *", json.getString("stop_schedule"))
        assertEquals(7L, json.getLong("depends_on"))
        assertEquals("/scripts/pre.sh", json.getString("task_before"))
        assertEquals("/scripts/post.sh", json.getString("task_after"))
        assertTrue(json.getBoolean("allow_multiple_instances"))
        assertEquals("queue", json.getString("schedule_policy"))
        assertEquals("3.11", json.getString("python_version"))
        assertTrue(json.getBoolean("notify_on_failure"))
        assertFalse(json.getBoolean("notify_on_success"))
        assertTrue(json.getBoolean("notify_on_abort"))
    }

    @Test fun `clamps out-of-range numeric fields`() {
        val json = TaskWritePayload(
            name = "clamp",
            scriptPath = "sh x.sh",
            schedule = "",
            timeout = 999999,
            maxRetries = 99,
            retryInterval = 999999,
        ).toJson()

        assertEquals(604800, json.getInt("timeout"))
        assertEquals(20, json.getInt("max_retries"))
        assertEquals(86400, json.getInt("retry_interval"))
    }

    @Test fun `null depends_on serializes as null and empty list stays empty`() {
        val json = TaskWritePayload(
            name = "no-dep",
            scriptPath = "sh x.sh",
            schedule = "",
        ).toJson()

        assertTrue(json.isNull("depends_on"))
        assertEquals(0, json.getJSONArray("labels").length())
        assertFalse(json.getBoolean("allow_multiple_instances"))
        assertEquals("skip", json.getString("schedule_policy"))
        assertEquals("0", json.getString("success_exit_codes"))
    }

    @Test fun `multi-line schedule splits into cron_expressions`() {
        val json = TaskWritePayload(
            name = "multi",
            scriptPath = "sh x.sh",
            schedule = "*/5 * * * *\n0 0 * * *",
        ).toJson()

        val expressions = json.getJSONArray("cron_expressions")
        assertEquals(2, expressions.length())
        assertEquals("*/5 * * * *", expressions.getString(0))
        assertEquals("0 0 * * *", expressions.getString(1))
    }

    @Test fun `task model round-trips advanced fields from detail json`() {
        val detail = JSONObject(
            """
            {
              "id": 9,
              "name": "r",
              "command": "sh x.sh",
              "cron_expression": "0 * * * *",
              "task_type": "cron",
              "status": 1,
              "timeout": 120,
              "max_retries": 2,
              "retry_interval": 30,
              "stop_schedule": "0 23 * * *",
              "depends_on": 4,
              "task_before": "pre.sh",
              "task_after": "post.sh",
              "allow_multiple_instances": true,
              "schedule_policy": "parallel",
              "python_version": "3.10",
              "notify_on_failure": true,
              "labels": ["a", "b"]
            }
            """.trimIndent(),
        )

        val task = Task.fromJson(detail)
        assertEquals(120, task.timeout)
        assertEquals(2, task.maxRetries)
        assertEquals(30, task.retryInterval)
        assertEquals("0 23 * * *", task.stopSchedule)
        assertEquals(4L, task.dependsOn)
        assertEquals("pre.sh", task.taskBefore)
        assertEquals("post.sh", task.taskAfter)
        assertTrue(task.allowMultipleInstances)
        assertEquals("parallel", task.schedulePolicy)
        assertEquals("3.10", task.pythonVersion)
        assertTrue(task.notifyOnFailure)
        assertEquals(listOf("a", "b"), task.labels)
    }

    @Test fun `task model handles absent advanced fields`() {
        val task = Task.fromJson(JSONObject("""{"id":1,"name":"n","command":"c"}"""))
        assertEquals(0, task.timeout)
        assertEquals(0, task.maxRetries)
        assertEquals(60, task.retryInterval)
        assertNull(task.dependsOn)
        assertEquals("", task.stopSchedule)
        assertEquals("", task.taskBefore)
        assertEquals("", task.taskAfter)
        assertFalse(task.allowMultipleInstances)
        assertEquals("skip", task.schedulePolicy)
    }
}
