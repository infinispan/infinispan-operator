package org.infinispan.example;

import org.infinispan.tasks.ServerTask;
import org.infinispan.tasks.TaskContext;

import java.nio.charset.StandardCharsets;

public class HelloTask03 implements ServerTask<byte[]> {
    private TaskContext ctx;

    @Override
    public void setTaskContext(TaskContext ctx) {
        this.ctx = ctx;
    }

    @Override
    public byte[] call() {
        String name = (String) ctx.getParameters().get().get("name");
        return ("Hello " + name).getBytes(StandardCharsets.UTF_8);
    }

    @Override
    public String getName() {
        return "task-03";
    }
}
