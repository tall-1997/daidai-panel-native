package com.daidai.daidai_app.data.repository

import com.daidai.daidai_app.data.api.DaidaiApi
import com.daidai.daidai_app.data.api.RetrofitClient
import com.daidai.daidai_app.data.model.Result
import com.daidai.daidai_app.data.model.Task
import com.daidai.daidai_app.data.model.TaskListResponse
import com.daidai.daidai_app.data.model.TaskStats
import com.daidai.daidai_app.data.model.TasksParams
import retrofit2.Retrofit

class TasksRepository {
    
    private val api: DaidaiApi by lazy {
        RetrofitClient.create()
    }
    
    suspend fun listTasks(params: TasksParams): Result<TaskListResponse> {
        return try {
            val response = api.getTasks()
            if (response.isSuccessful) {
                Result.Success(response.body() ?: TaskListResponse(emptyList(), 0))
            } else {
                Result.Error("请求失败: ${response.code()}")
            }
        } catch (e: Exception) {
            Result.Error(e.message ?: "网络错误")
        }
    }
    
    suspend fun getStats(taskId: Long): TaskStats {
        return try {
            val response = api.getTaskStats(taskId)
            if (response.isSuccessful) {
                response.body() ?: TaskStats()
            } else {
                TaskStats()
            }
        } catch (e: Exception) {
            TaskStats()
        }
    }
    
    suspend fun getTask(taskId: Long): Task? {
        return try {
            val response = api.getTask(taskId)
            if (response.isSuccessful) {
                response.body()
            } else {
                null
            }
        } catch (e: Exception) {
            null
        }
    }
}
