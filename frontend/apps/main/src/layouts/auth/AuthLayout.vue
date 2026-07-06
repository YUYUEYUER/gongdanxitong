<template>
  <div class="flex min-h-screen flex-col bg-secondary">
    <main class="flex-1 flex items-start justify-center pt-[15vh] p-4">
      <div class="w-full max-w-sm">
        <slot></slot>
      </div>
    </main>
    <div class="flex justify-center mb-6">
      <div class="text-xs text-center text-muted-foreground">Powered by lya</div>
    </div>
  </div>
</template>

<script setup></script>
