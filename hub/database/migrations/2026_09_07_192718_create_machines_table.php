<?php

use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     */
    public function up(): void
    {
        Schema::create('machines', function (Blueprint $table) {
            $table->id();
            $table->string('name');
            $table->string('host');
            $table->unsignedInteger('port')->default(8765);
            $table->enum('os', ['windows', 'linux', 'macos', 'other'])->default('other');
            $table->text('agent_token');
            $table->string('color', 7)->default('#8c7a63');
            $table->string('icon')->default('desktop');
            $table->unsignedInteger('sort_order')->default(0);
            $table->boolean('use_tls')->default(false);
            $table->string('status')->default('unknown');
            $table->timestamp('last_seen_at')->nullable();
            $table->text('notes')->nullable();
            $table->timestamps();
        });
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('machines');
    }
};
