<?php

use App\Models\Machine;
use App\Models\User;
use Illuminate\Database\Migrations\Migration;
use Illuminate\Database\Schema\Blueprint;
use Illuminate\Support\Facades\DB;
use Illuminate\Support\Facades\Schema;

return new class extends Migration
{
    /**
     * Run the migrations.
     *
     * Absence of a row means no access at all — every existing user is
     * backfilled with "full" on every existing machine here so upgrading
     * an install with data already in it doesn't lock the current user
     * out of their own machines.
     */
    public function up(): void
    {
        Schema::create('machine_user', function (Blueprint $table) {
            $table->id();
            $table->foreignIdFor(Machine::class)->constrained()->cascadeOnDelete();
            $table->foreignIdFor(User::class)->constrained()->cascadeOnDelete();
            $table->enum('role', ['read_only', 'full'])->default('read_only');
            $table->timestamps();
            $table->unique(['machine_id', 'user_id']);
        });

        $machineIds = Machine::query()->pluck('id');
        $userIds = User::query()->pluck('id');

        $rows = [];
        foreach ($userIds as $userId) {
            foreach ($machineIds as $machineId) {
                $rows[] = [
                    'machine_id' => $machineId,
                    'user_id' => $userId,
                    'role' => 'full',
                    'created_at' => now(),
                    'updated_at' => now(),
                ];
            }
        }

        if ($rows !== []) {
            DB::table('machine_user')->insert($rows);
        }
    }

    /**
     * Reverse the migrations.
     */
    public function down(): void
    {
        Schema::dropIfExists('machine_user');
    }
};
